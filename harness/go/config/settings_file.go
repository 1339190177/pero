package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	point "github.com/phodal/pero/harness/go/runtime"
	"gopkg.in/yaml.v3"
)

const (
	defaultSettingsWatchInterval = 100 * time.Millisecond
	defaultSettingsLockWait      = 30 * time.Second
	defaultSettingsLockPoll      = 25 * time.Millisecond
)

type FileSettingsOptions struct {
	Path          string
	DisableWatch  bool
	WatchInterval time.Duration
	LockWait      time.Duration
	LockPoll      time.Duration
	Logger        *slog.Logger
}

type settingsFileFormat uint8

const (
	settingsJSON settingsFileFormat = iota + 1
	settingsYAML
)

// FileSettingsBackend stores every raw namespace section in one JSON or YAML
// document. Writes take an inter-process lock, re-read the document, mutate
// only one namespace, and atomically replace the file.
type FileSettingsBackend struct {
	filename      string
	format        settingsFileFormat
	disableWatch  bool
	watchInterval time.Duration
	lockWait      time.Duration
	lockPoll      time.Duration
	logger        *slog.Logger

	mu           sync.Mutex
	watchStarted bool
}

func NewFileSettingsBackend(options FileSettingsOptions) (*FileSettingsBackend, error) {
	if options.Path == "" {
		return nil, errors.New("harness: settings file path is required")
	}
	filename, err := filepath.Abs(options.Path)
	if err != nil {
		return nil, err
	}
	var format settingsFileFormat
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".json":
		format = settingsJSON
	case ".yaml", ".yml":
		format = settingsYAML
	default:
		return nil, fmt.Errorf("harness: settings file %s must use .json, .yaml, or .yml", filename)
	}
	if options.WatchInterval == 0 {
		options.WatchInterval = defaultSettingsWatchInterval
	}
	if options.WatchInterval < 0 {
		return nil, errors.New("harness: settings watch interval must not be negative")
	}
	if options.LockWait == 0 {
		options.LockWait = defaultSettingsLockWait
	}
	if options.LockWait < 0 {
		return nil, errors.New("harness: settings lock wait must not be negative")
	}
	if options.LockPoll == 0 {
		options.LockPoll = defaultSettingsLockPoll
	}
	if options.LockPoll < time.Millisecond {
		return nil, errors.New("harness: settings lock poll must be at least one millisecond")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &FileSettingsBackend{
		filename: filename, format: format, disableWatch: options.DisableWatch,
		watchInterval: options.WatchInterval, lockWait: options.LockWait, lockPoll: options.LockPoll, logger: options.Logger,
	}, nil
}

func (b *FileSettingsBackend) Path() string {
	if b == nil {
		return ""
	}
	return b.filename
}

func (b *FileSettingsBackend) Load(context.Context) (map[string]json.RawMessage, error) {
	if b == nil {
		return nil, errors.New("harness: settings file backend is nil")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	document, _, err := b.loadUnlocked()
	return document, err
}

func (b *FileSettingsBackend) CommitSection(
	ctx context.Context,
	namespace string,
	mutation SettingsMutation,
) (map[string]json.RawMessage, error) {
	if b == nil {
		return nil, errors.New("harness: settings file backend is nil")
	}
	if err := validateSettingsNamespace(namespace); err != nil {
		return nil, err
	}
	if mutation == nil {
		return nil, errors.New("harness: settings file mutation must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(filepath.Dir(b.filename), 0o700); err != nil {
		return nil, err
	}
	release, err := b.acquireLock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	b.mu.Lock()
	defer b.mu.Unlock()
	document, source, err := b.loadUnlocked()
	if err != nil {
		return nil, err
	}
	current, found := document[namespace]
	section, err := mutation(append(json.RawMessage(nil), current...), found)
	if err != nil {
		return nil, err
	}
	if section == nil {
		return cloneSettingsDocument(document), nil
	}
	if _, err := decodeSettingsObject(section); err != nil {
		return nil, NewProviderError("settings file mutation returned a non-object section", InvalidSettingsCode, err)
	}
	if sameRawSettingsSection(current, found, section, true) {
		return cloneSettingsDocument(document), nil
	}
	document[namespace] = append(json.RawMessage(nil), section...)
	rendered, err := b.renderUnlocked(source, namespace, section, document)
	if err != nil {
		return nil, err
	}
	if err := atomicWriteSettingsFile(b.filename, rendered); err != nil {
		return nil, err
	}
	return cloneSettingsDocument(document), nil
}

func (b *FileSettingsBackend) loadUnlocked() (map[string]json.RawMessage, []byte, error) {
	info, err := os.Lstat(b.filename)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]json.RawMessage), nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("harness: settings file %s must not be a symlink", b.filename)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("harness: settings file %s must be a regular file", b.filename)
	}
	source, err := os.ReadFile(b.filename)
	if err != nil {
		return nil, nil, err
	}
	var document map[string]json.RawMessage
	switch b.format {
	case settingsJSON:
		document, err = parseJSONSettingsDocument(source)
	case settingsYAML:
		document, err = parseYAMLSettingsDocument(source)
	default:
		err = errors.New("harness: unsupported settings file format")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("harness: invalid settings document %s: %w", b.filename, err)
	}
	return document, source, nil
}

func parseJSONSettingsDocument(source []byte) (map[string]json.RawMessage, error) {
	if err := validateUniqueJSON(bytes.NewReader(source)); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	var document map[string]json.RawMessage
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, errors.New("document root must be an object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("document has trailing data")
	}
	return cloneSettingsDocument(document), nil
}

func validateUniqueJSON(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("JSON object key is not a string")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("JSON document has trailing data")
	}
	return nil
}

func parseYAMLSettingsDocument(source []byte) (map[string]json.RawMessage, error) {
	root, err := parseYAMLRoot(source)
	if err != nil {
		return nil, err
	}
	document := make(map[string]json.RawMessage)
	if len(root.Content) == 0 {
		return document, nil
	}
	mapping := root.Content[0]
	seen := make(map[string]struct{}, len(mapping.Content)/2)
	for index := 0; index < len(mapping.Content); index += 2 {
		key := mapping.Content[index]
		value := mapping.Content[index+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" {
			return nil, errors.New("document section keys must be non-empty strings")
		}
		if _, duplicate := seen[key.Value]; duplicate {
			return nil, fmt.Errorf("duplicate section %q", key.Value)
		}
		seen[key.Value] = struct{}{}
		var decoded any
		if err := value.Decode(&decoded); err != nil {
			return nil, err
		}
		decoded, err = normalizeYAMLValue(decoded)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(decoded)
		if err != nil {
			return nil, err
		}
		document[key.Value] = raw
	}
	return document, nil
}

func parseYAMLRoot(source []byte) (*yaml.Node, error) {
	root := &yaml.Node{}
	if len(bytes.TrimSpace(source)) == 0 {
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		return root, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	if err := decoder.Decode(root); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{{
			Kind: yaml.MappingNode, Tag: "!!map",
			HeadComment: strings.TrimRight(string(source), "\r\n"),
		}}
	}
	if root.Kind == 0 || (root.Kind == yaml.DocumentNode && len(root.Content) == 0) {
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("document root must be a mapping")
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		if len(trailing.Content) != 0 {
			return nil, errors.New("document must contain exactly one YAML document")
		}
	}
	return root, nil
}

func normalizeYAMLValue(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, entry := range typed {
			normalized, err := normalizeYAMLValue(entry)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, entry := range typed {
			stringKey, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("YAML settings map key %v is not a string", key)
			}
			normalized, err := normalizeYAMLValue(entry)
			if err != nil {
				return nil, err
			}
			result[stringKey] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, entry := range typed {
			normalized, err := normalizeYAMLValue(entry)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	case nil, bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return value, nil
	default:
		return nil, fmt.Errorf("YAML settings value has unsupported type %T", value)
	}
}

func (b *FileSettingsBackend) renderUnlocked(
	source []byte,
	namespace string,
	section json.RawMessage,
	document map[string]json.RawMessage,
) ([]byte, error) {
	if b.format == settingsJSON {
		return json.MarshalIndent(document, "", "  ")
	}
	root, err := parseYAMLRoot(source)
	if err != nil {
		return nil, err
	}
	mapping := root.Content[0]
	var value any
	if err := json.Unmarshal(section, &value); err != nil {
		return nil, err
	}
	next := &yaml.Node{}
	if err := next.Encode(value); err != nil {
		return nil, err
	}
	replaced := false
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value != namespace {
			continue
		}
		previous := mapping.Content[index+1]
		var current any
		if err := previous.Decode(&current); err != nil {
			return nil, err
		}
		current, err = normalizeYAMLValue(current)
		if err != nil {
			return nil, err
		}
		patched, patchErr := patchYAMLSettingsNode(previous, current, value)
		if patchErr != nil {
			return nil, patchErr
		}
		mapping.Content[index+1] = patched
		replaced = true
		break
	}
	if !replaced {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: namespace}, next,
		)
	}
	var rendered bytes.Buffer
	encoder := yaml.NewEncoder(&rendered)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return rendered.Bytes(), nil
}

// patchYAMLSettingsNode applies the same leaf-level map diff as DSH's
// settings-file provider. Unchanged nodes and mapping key nodes retain their
// comments, anchors, style, and ordering; arrays and scalars replace as one
// value when changed.
func patchYAMLSettingsNode(node *yaml.Node, current, next any) (*yaml.Node, error) {
	currentMap, currentIsMap := current.(map[string]any)
	nextMap, nextIsMap := next.(map[string]any)
	if currentIsMap && nextIsMap && node.Kind == yaml.MappingNode {
		content := make([]*yaml.Node, 0, len(node.Content))
		for index := 0; index < len(node.Content); index += 2 {
			if _, retained := nextMap[node.Content[index].Value]; retained {
				content = append(content, node.Content[index], node.Content[index+1])
			}
		}
		node.Content = content

		keys := make([]string, 0, len(nextMap))
		for key := range nextMap {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			index := yamlMappingValueIndex(node, key)
			if index >= 0 {
				patched, err := patchYAMLSettingsNode(node.Content[index], currentMap[key], nextMap[key])
				if err != nil {
					return nil, err
				}
				node.Content[index] = patched
				continue
			}
			value := &yaml.Node{}
			if err := value.Encode(nextMap[key]); err != nil {
				return nil, err
			}
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value,
			)
		}
		return node, nil
	}
	if reflect.DeepEqual(current, next) {
		return node, nil
	}
	replacement := &yaml.Node{}
	if err := replacement.Encode(next); err != nil {
		return nil, err
	}
	replacement.HeadComment = node.HeadComment
	replacement.LineComment = node.LineComment
	replacement.FootComment = node.FootComment
	return replacement, nil
}

func yamlMappingValueIndex(node *yaml.Node, key string) int {
	for index := 0; index < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return index + 1
		}
	}
	return -1
}

func atomicWriteSettingsFile(filename string, data []byte) error {
	directory := filepath.Dir(filename)
	temporary, err := os.CreateTemp(directory, ".pero-settings-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return err
	}
	committed = true
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

func (b *FileSettingsBackend) acquireLock(ctx context.Context) (func(), error) {
	lockPath := b.filename + ".lock"
	deadline := time.Now().Add(b.lockWait)
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if b.lockWait == 0 || !time.Now().Before(deadline) {
			return nil, fmt.Errorf("harness: timed out waiting for settings writer lock %s", lockPath)
		}
		timer := time.NewTimer(b.lockPoll)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

type settingsFingerprint struct {
	exists bool
	digest [sha256.Size]byte
}

func (b *FileSettingsBackend) fingerprint() (settingsFingerprint, error) {
	info, err := os.Lstat(b.filename)
	if errors.Is(err, os.ErrNotExist) {
		return settingsFingerprint{}, nil
	}
	if err != nil {
		return settingsFingerprint{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return settingsFingerprint{}, fmt.Errorf("harness: settings file %s must be a regular non-symlink file", b.filename)
	}
	source, err := os.ReadFile(b.filename)
	if err != nil {
		return settingsFingerprint{}, err
	}
	return settingsFingerprint{exists: true, digest: sha256.Sum256(source)}, nil
}

func (b *FileSettingsBackend) StartWatching(run *point.Run, provider *SettingsProvider) error {
	if b == nil || run == nil || !run.Active() || provider == nil {
		return point.ErrInactiveRun
	}
	if b.disableWatch {
		return nil
	}
	b.mu.Lock()
	if b.watchStarted {
		b.mu.Unlock()
		return errors.New("harness: settings file watcher already started")
	}
	initial, err := b.fingerprint()
	if err != nil {
		b.mu.Unlock()
		return err
	}
	b.watchStarted = true
	b.mu.Unlock()
	ctx, cancel := context.WithCancel(run.Context)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		last := initial
		ticker := time.NewTicker(b.watchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, fingerprintErr := b.fingerprint()
				if fingerprintErr != nil {
					b.logger.Warn("settings file watcher could not inspect document", "error", fingerprintErr)
					continue
				}
				if current == last {
					continue
				}
				last = current
				if reloadErr := provider.Reload(ctx); reloadErr != nil && ctx.Err() == nil {
					b.logger.Warn("settings file watcher kept last-good document", "error", reloadErr)
				}
			}
		}
	}()
	return run.Defer("settings-file:stop-watcher", func(context.Context) error {
		cancel()
		wait.Wait()
		return nil
	})
}

var _ SettingsBackend = (*FileSettingsBackend)(nil)
var _ SettingsWatchBackend = (*FileSettingsBackend)(nil)
