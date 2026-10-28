package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	point "github.com/phodal/pero/harness/go/runtime"
	harnesstool "github.com/phodal/pero/harness/go/tool"
)

type CommandID string

// CommandDefinitionID is optional plugin-owned discovery metadata. It is not
// an execution ID, owner incarnation, or authority to invoke a command.
type CommandDefinitionID string

// Only this additive metadata field uses the strict native string boundary.
// Existing command names, descriptions and input descriptors keep their codec.
func (id CommandDefinitionID) MarshalJSON() ([]byte, error) {
	if err := validateCommandDefinitionID(&id); err != nil {
		return nil, err
	}
	return json.Marshal(string(id))
}

func (id *CommandDefinitionID) UnmarshalJSON(raw []byte) error {
	if err := harnesstool.ValidateNativeJSONDecoding(raw); err != nil {
		return NewProviderError("command definition identity cannot enter a native string", "UNREPRESENTABLE_JSON_STRING", err)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*id = CommandDefinitionID(value)
	return nil
}

func validateCommandDefinitionID(id *CommandDefinitionID) error {
	if id != nil && !utf8.ValidString(string(*id)) {
		return NewProviderError("command definition identity cannot enter a native string", "UNREPRESENTABLE_JSON_STRING", nil)
	}
	return nil
}

type CommandResultKind string

const (
	CommandSuccess CommandResultKind = "success"
	CommandError   CommandResultKind = "error"
)

type CommandResult struct {
	Kind           CommandResultKind `json:"kind"`
	Text           string            `json:"text,omitempty"`
	SourceEventSeq uint64            `json:"sourceEventSeq,omitempty"`
}

type CommandExecution struct {
	CommandID CommandID     `json:"commandId"`
	Result    CommandResult `json:"result"`
}

type CommandInvocation struct {
	CommandID CommandID
	Agent     *Agent
	RawInput  string
	// Attachments contains durably admitted image/file blocks in submission order.
	// Current commands declare Input.Attachments; Images is the legacy image-only flag.
	Attachments []ContentBlock
}

type CommandInputDescriptor struct {
	Hint        string `json:"hint"`
	Images      bool   `json:"images,omitempty"`
	Attachments bool   `json:"attachments,omitempty"`
}

type CommandHandler func(context.Context, CommandInvocation) (CommandResult, error)

type CommandDefinition struct {
	// Exact native owner and optional stable dependency admission. Never wire data.
	profileLease *ProfileOwnerLease
	available    func() bool
	admit        func(context.Context) (context.Context, func(), error)
	// DefinitionID preserves an explicitly empty identity separately from absence.
	DefinitionID *CommandDefinitionID
	Name         string
	Description  string
	Input        *CommandInputDescriptor
	// OmitInput keeps command/run free of duplicated sensitive/domain-owned input.
	OmitInput bool
	Handler   CommandHandler
}

// CommandProvider contributes an Agent-specific command catalog whose
// implementation is owned outside the Go registry (for example, by one DSH
// Cordis graph). Resolve must return the same metadata advertised by List.
// The registry still owns command admission, durable run/done pairing,
// attachment admission, cancellation, and result validation.
type CommandProvider struct {
	List    func(context.Context, *Agent) ([]CommandDescriptor, error)
	Resolve func(context.Context, *Agent, string) (CommandDefinition, bool, error)
}

type CommandDescriptor struct {
	DefinitionID *CommandDefinitionID    `json:"definitionId,omitempty"`
	Name         string                  `json:"name"`
	Description  string                  `json:"description"`
	Input        *CommandInputDescriptor `json:"input,omitempty"`
}

type ParsedCommand struct {
	Name     string
	RawInput string
}

type commandEntry struct {
	id      uint64
	command CommandDefinition
	scope   *point.Scope
	owner   *commandOwner
}

type commandProviderEntry struct {
	id       uint64
	provider CommandProvider
	scope    *point.Scope
	owner    *commandOwner
}

type CommandRegistry struct {
	mu        sync.RWMutex
	nextID    uint64
	entries   map[string][]commandEntry
	providers []commandProviderEntry
	prefix    string
	issued    atomic.Uint64
	closed    atomic.Bool
	callbacks atomic.Int64
}

var commandNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var commandLinePattern = regexp.MustCompile(`^/([a-z][a-z0-9_-]*)`)
var fallbackCommandPrefix atomic.Uint64

func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{entries: make(map[string][]commandEntry), prefix: newCommandPrefix()}
}

func ParseCommand(line string) (ParsedCommand, bool) {
	match := commandLinePattern.FindStringSubmatchIndex(line)
	if match == nil {
		return ParsedCommand{}, false
	}
	end := match[1]
	if end < len(line) && !strings.ContainsRune("\t\n\r ", rune(line[end])) {
		return ParsedCommand{}, false
	}
	return ParsedCommand{Name: line[match[2]:match[3]], RawInput: line[end:]}, true
}

// Register binds the handler to this exact Run. The returned function only
// unregisters; owner cleanup separately cancels and joins admitted callbacks.
func (r *CommandRegistry) Register(run *point.Run, command CommandDefinition) (point.Dispose, error) {
	if run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	if err := validateCommandDefinitionID(command.DefinitionID); err != nil {
		return nil, err
	}
	if !commandNamePattern.MatchString(command.Name) {
		return nil, fmt.Errorf("harness: command name %q must match %s", command.Name, commandNamePattern)
	}
	if strings.TrimSpace(command.Description) == "" {
		return nil, fmt.Errorf("harness: command %q description must not be empty", command.Name)
	}
	if command.Handler == nil {
		return nil, fmt.Errorf("harness: command %q requires a handler", command.Name)
	}
	if command.Input != nil && strings.TrimSpace(command.Input.Hint) == "" {
		return nil, fmt.Errorf("harness: command %q input hint must not be empty", command.Name)
	}
	command.DefinitionID = cloneCommandDefinitionID(command.DefinitionID)
	command.Input = cloneCommandInput(command.Input)
	owner := newCommandOwner(r, run)
	command.Handler = owner.wrapHandler(command.Handler)
	if err := run.BeforeCleanup("command-owner:"+command.Name, owner.drain); err != nil {
		return nil, err
	}
	r.mu.Lock()
	owner.mu.Lock()
	if err := owner.admissionErrorLocked(); err != nil {
		owner.mu.Unlock()
		r.mu.Unlock()
		return nil, err
	}
	owner.mu.Unlock()
	for _, existing := range r.entries[command.Name] {
		if existing.scope == run.Scope {
			r.mu.Unlock()
			return nil, fmt.Errorf("harness: command %q already registered in this scope", command.Name)
		}
	}
	r.nextID++
	entry := commandEntry{id: r.nextID, command: command, scope: run.Scope, owner: owner}
	r.entries[command.Name] = append(r.entries[command.Name], entry)
	r.mu.Unlock()

	var once sync.Once
	dispose := point.Dispose(func(context.Context) error {
		once.Do(func() {
			owner.unregister()
			r.mu.Lock()
			entries := r.entries[command.Name]
			for index, current := range entries {
				if current.id == entry.id {
					entries = append(entries[:index], entries[index+1:]...)
					break
				}
			}
			if len(entries) == 0 {
				delete(r.entries, command.Name)
			} else {
				r.entries[command.Name] = entries
			}
			r.mu.Unlock()
		})
		return nil
	})
	if err := run.Defer("command:"+command.Name, dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

// RegisterProvider installs a dynamic, Agent-scoped command source. Providers
// are called without registry/owner locks held. Exact activation cleanup
// cancels and joins their callback bodies before releasing ordinary resources.
// The returned function only unregisters; it never cancels or joins callbacks.
func (r *CommandRegistry) RegisterProvider(run *point.Run, provider CommandProvider) (point.Dispose, error) {
	if run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	if provider.List == nil || provider.Resolve == nil {
		return nil, errors.New("harness: command provider requires list and resolve")
	}
	owner := newCommandOwner(r, run)
	if err := run.BeforeCleanup("command-provider-owner", owner.drain); err != nil {
		return nil, err
	}
	r.mu.Lock()
	owner.mu.Lock()
	if err := owner.admissionErrorLocked(); err != nil {
		owner.mu.Unlock()
		r.mu.Unlock()
		return nil, err
	}
	owner.mu.Unlock()
	r.nextID++
	entry := commandProviderEntry{id: r.nextID, provider: provider, scope: run.Scope, owner: owner}
	r.providers = append(r.providers, entry)
	r.mu.Unlock()

	var once sync.Once
	dispose := point.Dispose(func(context.Context) error {
		once.Do(func() {
			owner.unregister()
			r.mu.Lock()
			for index := range r.providers {
				if r.providers[index].id == entry.id {
					r.providers = append(r.providers[:index], r.providers[index+1:]...)
					break
				}
			}
			r.mu.Unlock()
		})
		return nil
	})
	if err := run.Defer("command-provider", dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (r *CommandRegistry) List(scope *point.Scope) []CommandDescriptor {
	r.mu.RLock()
	result := make([]CommandDescriptor, 0, len(r.entries))
	for _, entries := range r.entries {
		entry, ok := nearestCommandEntry(entries, scope)
		if ok {
			result = append(result, CommandDescriptor{
				DefinitionID: cloneCommandDefinitionID(entry.command.DefinitionID),
				Name:         entry.command.Name, Description: entry.command.Description, Input: cloneCommandInput(entry.command.Input),
			})
		}
	}
	r.mu.RUnlock()
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}

// ListForAgent returns the merged static and dynamic catalog for one exact
// Agent. List remains the allocation-light static lookup used by callers that
// have only a scope; external providers require this Agent-aware form.
func (r *CommandRegistry) ListForAgent(ctx context.Context, agent *Agent) ([]CommandDescriptor, error) {
	if agent == nil {
		return nil, errors.New("harness: command catalog requires an Agent")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	operation, err := agent.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer operation.release()
	ctx = operation.context
	result := r.List(agent.Scope())
	byName := make(map[string]struct{}, len(result))
	for _, descriptor := range result {
		byName[descriptor.Name] = struct{}{}
	}
	for _, entry := range r.providerSnapshot(agent.Scope()) {
		if err := agent.operationAdmissionError(); err != nil {
			return nil, err
		}
		descriptors, err := entry.list(ctx, agent)
		if err := agent.operationAdmissionError(); err != nil {
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		for _, descriptor := range descriptors {
			if err := validateCommandDescriptor(descriptor); err != nil {
				return nil, err
			}
			if _, duplicate := byName[descriptor.Name]; duplicate {
				return nil, fmt.Errorf("harness: command %q is provided by more than one catalog", descriptor.Name)
			}
			descriptor.DefinitionID = cloneCommandDefinitionID(descriptor.DefinitionID)
			descriptor.Input = cloneCommandInput(descriptor.Input)
			result = append(result, descriptor)
			byName[descriptor.Name] = struct{}{}
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

func (r *CommandRegistry) Resolve(scope *point.Scope, name string) (CommandDefinition, bool) {
	r.mu.RLock()
	entry, ok := nearestCommandEntry(r.entries[name], scope)
	r.mu.RUnlock()
	if !ok {
		return CommandDefinition{}, false
	}
	command := entry.command
	command.DefinitionID = cloneCommandDefinitionID(command.DefinitionID)
	command.Input = cloneCommandInput(command.Input)
	return command, true
}

func (r *CommandRegistry) resolveForAgent(ctx context.Context, agent *Agent, name string) (CommandDefinition, bool, error) {
	static, staticFound := r.Resolve(agent.Scope(), name)
	resolved := static
	found := staticFound
	for _, entry := range r.providerSnapshot(agent.Scope()) {
		if err := agent.operationAdmissionError(); err != nil {
			return CommandDefinition{}, false, err
		}
		candidate, ok, err := entry.resolve(ctx, agent, name)
		if err := agent.operationAdmissionError(); err != nil {
			return CommandDefinition{}, false, err
		}
		if err != nil {
			return CommandDefinition{}, false, err
		}
		if !ok || !candidate.ownerAvailable() {
			continue
		}
		if found {
			return CommandDefinition{}, false, fmt.Errorf("harness: command %q is provided by more than one catalog", name)
		}
		if candidate.Name != name {
			return CommandDefinition{}, false, fmt.Errorf("harness: command provider resolved %q for requested command %q", candidate.Name, name)
		}
		if err := validateCommandDefinition(candidate); err != nil {
			return CommandDefinition{}, false, err
		}
		candidate.DefinitionID = cloneCommandDefinitionID(candidate.DefinitionID)
		candidate.Input = cloneCommandInput(candidate.Input)
		resolved, found = candidate, true
	}
	return resolved, found, nil
}

func (r *CommandRegistry) providerSnapshot(scope *point.Scope) []commandProviderEntry {
	r.mu.RLock()
	providers := make([]commandProviderEntry, 0, len(r.providers))
	for _, entry := range r.providers {
		if entry.scope.Contains(scope) && entry.owner.admissionError() == nil {
			providers = append(providers, entry)
		}
	}
	r.mu.RUnlock()
	return providers
}

func validateCommandDescriptor(descriptor CommandDescriptor) error {
	return validateCommandDefinition(CommandDefinition{
		DefinitionID: descriptor.DefinitionID,
		Name:         descriptor.Name, Description: descriptor.Description, Input: descriptor.Input,
		Handler: func(context.Context, CommandInvocation) (CommandResult, error) {
			return CommandResult{Kind: CommandSuccess}, nil
		},
	})
}

func validateCommandDefinition(command CommandDefinition) error {
	if err := validateCommandDefinitionID(command.DefinitionID); err != nil {
		return err
	}
	if !commandNamePattern.MatchString(command.Name) {
		return fmt.Errorf("harness: command name %q must match %s", command.Name, commandNamePattern)
	}
	if strings.TrimSpace(command.Description) == "" {
		return fmt.Errorf("harness: command %q description must not be empty", command.Name)
	}
	if command.Handler == nil {
		return fmt.Errorf("harness: command %q requires a handler", command.Name)
	}
	if command.Input != nil && strings.TrimSpace(command.Input.Hint) == "" {
		return fmt.Errorf("harness: command %q input hint must not be empty", command.Name)
	}
	return nil
}

func cloneCommandDefinitionID(id *CommandDefinitionID) *CommandDefinitionID {
	if id == nil {
		return nil
	}
	copy := *id
	return &copy
}

func cloneCommandInput(input *CommandInputDescriptor) *CommandInputDescriptor {
	if input == nil {
		return nil
	}
	copy := *input
	return &copy
}

// Execute resolves one slash command without sending it to the model. Syntax
// and name misses write nothing; admitted invocations always write a paired
// command/run and command/done lifecycle outside any synthetic turn.
func (r *CommandRegistry) Execute(ctx context.Context, agent *Agent, line string, images ...EncodedImageAttachment) (CommandExecution, bool, error) {
	submitted := make([]CommandSubmitAttachment, 0, len(images))
	for _, image := range images {
		submitted = append(submitted, CommandSubmitAttachment{Type: "image", Data: image.Data, MediaType: image.MediaType, Name: image.Name})
	}
	return r.ExecuteWithAttachments(ctx, agent, line, submitted, nil)
}

// CommandSubmitAttachment is the current ordered command wire union. File
// references are minted only by the supplied exact-session receipt resolver.
type CommandSubmitAttachment struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Name      string `json:"name,omitempty"`
	ReceiptID string `json:"receiptId,omitempty"`
}
type CommandFileReceiptResolver func(*Agent, string) (FileAttachmentRef, bool)

func (r *CommandRegistry) ExecuteWithAttachments(ctx context.Context, agent *Agent, line string, submitted []CommandSubmitAttachment, resolveFile CommandFileReceiptResolver) (CommandExecution, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	parsed, ok := ParseCommand(line)
	if !ok || agent == nil {
		return CommandExecution{}, false, nil
	}
	operation, err := agent.beginOperation(ctx)
	if err != nil {
		return CommandExecution{}, true, err
	}
	defer operation.release()
	ctx = operation.context
	command, ok, err := r.resolveForAgent(ctx, agent, parsed.Name)
	if retiredErr := agent.operationAdmissionError(); retiredErr != nil {
		return CommandExecution{}, true, retiredErr
	}
	if err != nil {
		return CommandExecution{}, true, err
	}
	if !ok {
		return CommandExecution{}, false, nil
	}
	if err := commandContextFailure(ctx); err != nil {
		return CommandExecution{}, true, err
	}
	ctx, releaseOwner, err := command.beginOwnerOperation(ctx)
	if err != nil {
		return CommandExecution{}, true, err
	}
	defer releaseOwner() // includes command/run, attachments and the command/done append
	commandID := CommandID(fmt.Sprintf("cmd-%s-%d", r.prefix, r.issued.Add(1)))
	runPayload := struct {
		CommandID CommandID      `json:"commandId"`
		Name      string         `json:"name"`
		Args      *string        `json:"args,omitempty"`
		Source    map[string]any `json:"source"`
	}{CommandID: commandID, Name: parsed.Name, Source: map[string]any{"kind": "user"}}
	if !command.OmitInput {
		raw := parsed.RawInput
		runPayload.Args = &raw
	}
	if _, err := agent.Session().Append("command/run", runPayload); err != nil {
		return CommandExecution{}, true, err
	}
	attachments := make([]ContentBlock, 0, len(submitted))
	var admissionResult *CommandResult
	admissionErr := agent.operationAdmissionError()
	if admissionErr == nil && len(submitted) != 0 {
		reject := func(text string) { result := CommandResult{Kind: CommandError, Text: text}; admissionResult = &result }
		allImages := true
		for _, part := range submitted {
			if part.Type != "image" {
				allImages = false
			}
		}
		if command.Input == nil || (!command.Input.Attachments && !(allImages && command.Input.Images)) {
			kind := "attachments"
			if allImages {
				kind = "image attachments"
			}
			reject(fmt.Sprintf("/%s does not accept %s", parsed.Name, kind))
		} else {
			images := make([]EncodedImageAttachment, 0, len(submitted))
			files := map[string]FileAttachmentRef{}
			for _, part := range submitted {
				if part.Type == "file" {
					if part.ReceiptID == "" || part.Data != "" || part.MediaType != "" || part.Name != "" || resolveFile == nil {
						reject("File upload receipt is unknown for this session.")
						break
					}
					ref, found := resolveFile(agent, part.ReceiptID)
					if !found {
						reject("File upload receipt is unknown for this session.")
						break
					}
					files[part.ReceiptID] = ref
				} else if part.Type == "image" && part.ReceiptID == "" {
					images = append(images, EncodedImageAttachment{Data: part.Data, MediaType: part.MediaType, Name: part.Name})
				} else {
					reject("Command attachment is invalid.")
					break
				}
			}
			var refs []ImageAttachmentRef
			if admissionResult == nil && len(images) > 0 {
				if agent.attachments == nil {
					reject(fmt.Sprintf("/%s: image attachments are unavailable because no attachment store is composed", parsed.Name))
				} else {
					var err error
					refs, err = AdmitEncodedImages(ctx, agent.attachments, images)
					if err != nil {
						var attachmentErr *AttachmentError
						if errors.As(err, &attachmentErr) {
							reject(attachmentErr.Error())
						} else {
							admissionErr = err
						}
					}
				}
			}
			if admissionResult == nil && admissionErr == nil {
				imageIndex := 0
				for _, part := range submitted {
					if part.Type == "image" {
						ref := refs[imageIndex]
						imageIndex++
						attachments = append(attachments, ContentBlock{Type: BlockImage, Attachment: &ref})
					} else {
						ref := files[part.ReceiptID]
						attachments = append(attachments, ContentBlock{Type: BlockFile, FileAttachment: &ref})
					}
				}
				admissionErr = commandContextFailure(ctx)
			}
		}
	}
	type outcome struct {
		result CommandResult
		err    error
	}
	var result CommandResult
	var handlerErr error
	if admissionResult != nil {
		result = *admissionResult
	} else if admissionErr != nil {
		handlerErr = admissionErr
	} else if retiredErr := agent.operationAdmissionError(); retiredErr != nil {
		handlerErr = retiredErr
	} else {
		settled := make(chan outcome, 1)
		operation.retain()
		go func() {
			defer operation.release()
			completed := outcome{}
			defer func() {
				if recovered := recover(); recovered != nil {
					completed.err = fmt.Errorf("harness: command handler panic: %v", recovered)
				}
				settled <- completed
			}()
			if err := agent.operationAdmissionError(); err != nil {
				completed.err = err
				return
			}
			if err := commandContextFailure(ctx); err != nil {
				completed.err = err
				return
			}
			invocationAttachments := cloneContentBlocks(attachments)
			completed.result, completed.err = command.Handler(ctx, CommandInvocation{
				CommandID: commandID, Agent: agent, RawInput: parsed.RawInput, Attachments: invocationAttachments,
			})
		}()
		select {
		case completed := <-settled:
			result, handlerErr = completed.result, completed.err
			if err := commandContextFailure(ctx); err != nil {
				handlerErr = err
			}
		case <-ctx.Done():
			handlerErr = commandContextFailure(ctx)
		}
	}
	if handlerErr == nil {
		handlerErr = agent.operationAdmissionError()
	}
	if handlerErr == nil {
		handlerErr = validateCommandResult(parsed.Name, result)
	}
	if handlerErr != nil {
		result = CommandResult{Kind: CommandError, Text: handlerErr.Error()}
	}
	_, appendErr := agent.Session().Append("command/done", struct {
		CommandID      CommandID         `json:"commandId"`
		Kind           CommandResultKind `json:"kind"`
		Text           string            `json:"text,omitempty"`
		SourceEventSeq uint64            `json:"sourceEventSeq,omitempty"`
	}{CommandID: commandID, Kind: result.Kind, Text: result.Text, SourceEventSeq: result.SourceEventSeq})
	if handlerErr != nil {
		return CommandExecution{}, true, handlerErr
	}
	if appendErr != nil {
		return CommandExecution{}, true, appendErr
	}
	if err := agent.operationAdmissionError(); err != nil {
		return CommandExecution{}, true, err
	}
	return CommandExecution{CommandID: commandID, Result: result}, true, nil
}

func validateCommandResult(name string, result CommandResult) error {
	switch result.Kind {
	case CommandSuccess:
		return nil
	case CommandError:
		if strings.TrimSpace(result.Text) == "" {
			return fmt.Errorf("harness: command %q error text must not be empty", name)
		}
		if result.SourceEventSeq != 0 {
			return fmt.Errorf("harness: command %q error cannot reference a source event", name)
		}
		return nil
	default:
		return fmt.Errorf("harness: command %q returned unknown result kind %q", name, result.Kind)
	}
}

func nearestCommandEntry(entries []commandEntry, scope *point.Scope) (commandEntry, bool) {
	var selected commandEntry
	found := false
	for _, entry := range entries {
		if !entry.scope.Contains(scope) || entry.owner.admissionError() != nil || !entry.command.ownerAvailable() {
			continue
		}
		if !found || selected.scope.Contains(entry.scope) {
			selected, found = entry, true
		}
	}
	return selected, found
}

func commandContextFailure(ctx context.Context) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

func newCommandPrefix() string {
	var value [4]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("fallback-%d", fallbackCommandPrefix.Add(1))
}
