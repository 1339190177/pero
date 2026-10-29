// Package config owns launch-time environment and outbound network policy.
// It is independent of the Harness composition root so commands, plugins, and
// providers can share the same immutable configuration contracts.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type LaunchEnvironmentSource string

const (
	LaunchEnvironmentProcess LaunchEnvironmentSource = "process"
	LaunchEnvironmentProject LaunchEnvironmentSource = "project-env"
	LaunchEnvironmentUser    LaunchEnvironmentSource = "user-env"
)

var launchEnvironmentSourceOrder = [...]LaunchEnvironmentSource{
	LaunchEnvironmentProcess,
	LaunchEnvironmentProject,
	LaunchEnvironmentUser,
}

type LaunchEnvironmentLayer struct {
	Source LaunchEnvironmentSource
	Path   string
	Values map[string]string
}

type LaunchEnvironmentEntry struct {
	Value  string
	Source LaunchEnvironmentSource
	Path   string
}

type launchEnvironmentLayer struct {
	path   string
	values map[string]string
}

// LaunchEnvironmentSnapshot is an immutable launch-time view. It preserves
// source provenance instead of flattening dotenv values into process env.
type LaunchEnvironmentSnapshot struct {
	bySource map[LaunchEnvironmentSource]launchEnvironmentLayer
}

func NewLaunchEnvironmentSnapshot(layers []LaunchEnvironmentLayer) (*LaunchEnvironmentSnapshot, error) {
	result := &LaunchEnvironmentSnapshot{bySource: make(map[LaunchEnvironmentSource]launchEnvironmentLayer, len(layers))}
	for _, layer := range layers {
		if !validLaunchEnvironmentSource(layer.Source) {
			return nil, fmt.Errorf("harness: unknown launch environment source %q", layer.Source)
		}
		values := make(map[string]string, len(layer.Values))
		for name, value := range layer.Values {
			values[launchEnvironmentLookupKey(name)] = value
		}
		result.bySource[layer.Source] = launchEnvironmentLayer{path: layer.Path, values: values}
	}
	return result, nil
}

func validLaunchEnvironmentSource(source LaunchEnvironmentSource) bool {
	switch source {
	case LaunchEnvironmentProcess, LaunchEnvironmentProject, LaunchEnvironmentUser:
		return true
	default:
		return false
	}
}

func launchEnvironmentLookupKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func (s *LaunchEnvironmentSnapshot) Get(name string) (LaunchEnvironmentEntry, bool) {
	return s.getFromMask(name, 0b111)
}

// GetFrom retains canonical trust order even when sources are supplied in a
// different order.
func (s *LaunchEnvironmentSnapshot) GetFrom(name string, sources ...LaunchEnvironmentSource) (LaunchEnvironmentEntry, bool) {
	var mask uint8
	for _, source := range sources {
		switch source {
		case LaunchEnvironmentProcess:
			mask |= 0b001
		case LaunchEnvironmentProject:
			mask |= 0b010
		case LaunchEnvironmentUser:
			mask |= 0b100
		}
	}
	return s.getFromMask(name, mask)
}

func (s *LaunchEnvironmentSnapshot) getFromMask(name string, mask uint8) (LaunchEnvironmentEntry, bool) {
	if s == nil {
		return LaunchEnvironmentEntry{}, false
	}
	key := launchEnvironmentLookupKey(name)
	for index, source := range launchEnvironmentSourceOrder {
		if mask&(1<<index) == 0 {
			continue
		}
		layer, found := s.bySource[source]
		if !found {
			continue
		}
		value, found := layer.values[key]
		if !found {
			continue
		}
		return LaunchEnvironmentEntry{Value: value, Source: source, Path: layer.path}, true
	}
	return LaunchEnvironmentEntry{}, false
}

type LayeredEnvironmentOptions struct {
	BinName   string
	CWD       string
	Home      string
	Environ   []string
	LookupEnv func(string) (string, bool)
	SetEnv    func(string, string) error
	Warn      func(string)
}

var bootstrapEnvironmentNames = map[string]struct{}{
	"PATH": {}, "HOME": {}, "USERPROFILE": {}, "SHELL": {},
	"NODE_OPTIONS": {}, "NODE_PATH": {}, "NODE_EXTRA_CA_CERTS": {},
	"LD_PRELOAD": {}, "LD_LIBRARY_PATH": {}, "LD_AUDIT": {},
	"BASH_ENV": {}, "ENV": {}, "SHELLOPTS": {}, "BASHOPTS": {},
	"PERL5OPT": {}, "PERL5LIB": {}, "PYTHONSTARTUP": {}, "PYTHONPATH": {},
	"RUBYOPT": {}, "RUBYLIB": {}, "JAVA_TOOL_OPTIONS": {}, "_JAVA_OPTIONS": {},
	"JDK_JAVA_OPTIONS": {}, "PYTHONHOME": {},
	"GIT_SSH": {}, "GIT_SSH_COMMAND": {}, "GIT_EXTERNAL_DIFF": {},
	"GIT_PAGER": {}, "GIT_EDITOR": {}, "GIT_ASKPASS": {}, "SSH_ASKPASS": {},
	"GIT_CONFIG_GLOBAL": {}, "GIT_CONFIG_SYSTEM": {}, "GIT_CONFIG_COUNT": {},
	"EDITOR": {}, "VISUAL": {}, "PAGER": {}, "BROWSER": {},
	"DEEPSEEK_BASE_URL": {}, "DEEPSEEK_SEARCH_BASE_URL": {},
	"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {}, "HTTP_PROXY": {}, "HTTPS_PROXY": {},
	"ALL_PROXY": {}, "NO_PROXY": {}, "REQUESTS_CA_BUNDLE": {}, "CURL_CA_BUNDLE": {},
	"NODE_TLS_REJECT_UNAUTHORIZED": {},
}

var bootstrapEnvironmentPrefixes = [...]string{"DSH_", "XDG_", "DYLD_", "BASH_FUNC_"}

var proxyEnvironmentNames = map[string]struct{}{
	"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {}, "NO_PROXY": {},
}

func isProxyEnvironmentName(name string) bool {
	_, found := proxyEnvironmentNames[strings.ToUpper(name)]
	return found
}

func isBootstrapEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	if _, found := bootstrapEnvironmentNames[upper]; found {
		return true
	}
	for _, prefix := range bootstrapEnvironmentPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

type loadedEnvironmentLayer struct {
	path   string
	values map[string]string
}

// LoadLayeredEnvironment loads and freezes the DSH launch environment. Both
// dotenv layers are parsed and validated before either is materialized.
func LoadLayeredEnvironment(options LayeredEnvironmentOptions) (*LaunchEnvironmentSnapshot, error) {
	binName := strings.TrimSpace(options.BinName)
	if binName == "" {
		binName = "harness"
	}
	cwd := options.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	inherited := parseEnvironmentList(options.Environ)
	if options.Environ == nil {
		inherited = parseEnvironmentList(os.Environ())
	}
	home := options.Home
	if home == "" {
		return nil, fmt.Errorf("explicit environment home is required; automatic DSH/Point configuration discovery is disabled")
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	project, err := readEnvironmentLayer(binName, cwd, options.Warn, false)
	if err != nil {
		return nil, err
	}
	var user *loadedEnvironmentLayer
	if filepath.Clean(home) != filepath.Clean(cwd) {
		user, err = readEnvironmentLayer(binName, home, options.Warn, true)
		if err != nil {
			return nil, err
		}
	}
	layers := []LaunchEnvironmentLayer{{Source: LaunchEnvironmentProcess, Values: inherited}}
	if project != nil {
		layers = append(layers, LaunchEnvironmentLayer{Source: LaunchEnvironmentProject, Path: project.path, Values: project.values})
	}
	if user != nil {
		layers = append(layers, LaunchEnvironmentLayer{Source: LaunchEnvironmentUser, Path: user.path, Values: user.values})
	}
	snapshot, err := NewLaunchEnvironmentSnapshot(layers)
	if err != nil {
		return nil, err
	}
	lookup := options.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	set := options.SetEnv
	if set == nil {
		set = os.Setenv
	}
	for _, layer := range []*loadedEnvironmentLayer{project, user} {
		if layer == nil {
			continue
		}
		for name, value := range layer.values {
			if _, found := lookup(name); found {
				continue
			}
			if err := set(name, value); err != nil {
				return nil, err
			}
		}
	}
	return snapshot, nil
}

func parseEnvironmentList(entries []string) map[string]string {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		values[launchEnvironmentLookupKey(name)] = value
	}
	return values
}

func readEnvironmentLayer(binName, directory string, warn func(string), allowProxy bool) (*loadedEnvironmentLayer, error) {
	path := filepath.Join(directory, ".env")
	source, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		if warn != nil {
			warn(fmt.Sprintf("%s: failed to load .env: %v", binName, err))
		}
		return nil, nil
	}
	values := parseNodeEnvironment(source)
	for name := range values {
		if !isBootstrapEnvironmentName(name) {
			continue
		}
		if allowProxy && isProxyEnvironmentName(name) {
			continue
		}
		return nil, fmt.Errorf(
			"%s: %s sets %q, which only the launching environment may set; export %s instead of putting it in a .env file",
			binName, path, name, name,
		)
	}
	return &loadedEnvironmentLayer{path: path, values: values}, nil
}

// parseNodeEnvironment mirrors the parser used by Node's util.parseEnv: it
// does not interpolate $NAME, accepts multiline quotes, expands only literal
// \n inside double quotes, ignores export prefixes, and lets the last key win.
func parseNodeEnvironment(source []byte) map[string]string {
	content := strings.ReplaceAll(string(source), "\r", "")
	content = strings.Trim(content, " \t\n")
	values := make(map[string]string)
	for content != "" {
		if content[0] == '\n' || content[0] == '#' {
			if newline := strings.IndexByte(content, '\n'); newline >= 0 {
				content = content[newline+1:]
			} else {
				content = ""
			}
			continue
		}
		equal := strings.IndexByte(content, '=')
		newline := strings.IndexByte(content, '\n')
		if equal < 0 || (newline >= 0 && newline < equal) {
			if newline >= 0 {
				content = strings.Trim(content[newline+1:], " \t\n")
				continue
			}
			break
		}
		key := strings.Trim(content[:equal], " \t\n")
		content = content[equal+1:]
		if content == "" || content[0] == '\n' {
			values[key] = ""
			continue
		}
		content = strings.Trim(content, " \t\n")
		if key == "" {
			continue
		}
		if strings.HasPrefix(key, "export ") {
			key = strings.Trim(key[len("export "):], " \t\n")
		}
		if content == "" {
			values[key] = ""
			break
		}
		quote := content[0]
		if quote == '"' {
			if closing := strings.IndexByte(content[1:], quote); closing >= 0 {
				closing++
				value := strings.ReplaceAll(content[1:closing], `\n`, "\n")
				values[key] = value
				if next := strings.IndexByte(content[closing+1:], '\n'); next >= 0 {
					content = content[closing+1+next+1:]
				} else {
					content = ""
				}
				continue
			}
		}
		if quote == '\'' || quote == '"' || quote == '`' {
			closing := strings.IndexByte(content[1:], quote)
			if closing < 0 {
				if next := strings.IndexByte(content, '\n'); next >= 0 {
					values[key] = content[:next]
					content = content[next+1:]
				} else {
					values[key] = content
					break
				}
			} else {
				closing++
				values[key] = content[1:closing]
				if next := strings.IndexByte(content[closing+1:], '\n'); next >= 0 {
					content = content[closing+1+next+1:]
				} else {
					content = ""
				}
				continue
			}
		} else {
			if next := strings.IndexByte(content, '\n'); next >= 0 {
				value := content[:next]
				if hash := strings.IndexByte(value, '#'); hash >= 0 {
					value = value[:hash]
				}
				values[key] = strings.Trim(value, " \t\n")
				content = content[next+1:]
			} else {
				value := content
				if hash := strings.IndexByte(value, '#'); hash >= 0 {
					value = value[:hash]
				}
				values[key] = strings.Trim(value, " \t\n")
				content = ""
			}
		}
		content = strings.Trim(content, " \t\n")
	}
	return values
}
