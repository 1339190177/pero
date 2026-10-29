package config

import (
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"strings"
	"sync"
)

// OutboundProxyDiagnostic describes one proxy value that the Harness refused.
// Message intentionally never contains the value because proxy URLs may carry
// credentials.
type OutboundProxyDiagnostic struct {
	Kind    string
	Origin  string
	Message string
}

// OutboundProxyPolicy is the immutable process-wide HTTP(S) routing decision.
// Its fields stay private so credentials embedded in a proxy URL cannot leak
// through logging or JSON serialization.
type OutboundProxyPolicy struct {
	httpProxy  *url.URL
	httpsProxy *url.URL
	noProxy    string
	source     bool
	inherited  map[string]*string
}

type OutboundProxyResolution struct {
	Policy      OutboundProxyPolicy
	Diagnostics []OutboundProxyDiagnostic
}

type proxyCandidateKind uint8

const (
	proxyAbsent proxyCandidateKind = iota
	proxyAccepted
	proxyRejected
)

type proxyCandidate struct {
	kind  proxyCandidateKind
	value *url.URL
}

var loopbackNoProxy = [...]string{"localhost", "127.0.0.1", "::1", "[::1]"}

var outboundProxyEnvironmentNames = [...]string{
	"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY",
	"no_proxy", "NO_PROXY", "all_proxy", "ALL_PROXY",
}

func proxyEnvironmentEntry(snapshot *LaunchEnvironmentSnapshot, lower string) (LaunchEnvironmentEntry, string, bool) {
	for _, name := range [...]string{lower, strings.ToUpper(lower)} {
		entry, found := snapshot.GetFrom(name, LaunchEnvironmentProcess, LaunchEnvironmentUser)
		if found && strings.TrimSpace(entry.Value) != "" {
			entry.Value = strings.TrimSpace(entry.Value)
			return entry, name, true
		}
	}
	return LaunchEnvironmentEntry{}, "", false
}

func acceptProxyCandidate(snapshot *LaunchEnvironmentSnapshot, lower string, diagnostics *[]OutboundProxyDiagnostic) proxyCandidate {
	entry, origin, found := proxyEnvironmentEntry(snapshot, lower)
	if !found {
		return proxyCandidate{kind: proxyAbsent}
	}
	parsed, err := url.Parse(entry.Value)
	if err != nil || parsed.Host == "" {
		*diagnostics = append(*diagnostics, OutboundProxyDiagnostic{
			Kind: "invalid", Origin: origin,
			Message: fmt.Sprintf("%s is not a valid URL; connecting directly", origin),
		})
		return proxyCandidate{kind: proxyRejected}
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return proxyCandidate{kind: proxyAccepted, value: parsed}
	case "socks", "socks4", "socks4a", "socks5", "socks5h":
		*diagnostics = append(*diagnostics, OutboundProxyDiagnostic{
			Kind: "socks", Origin: origin,
			Message: fmt.Sprintf("%s names a SOCKS proxy, which is not supported; connecting directly for that scheme — set an http:// or https:// proxy URL instead", origin),
		})
	default:
		*diagnostics = append(*diagnostics, OutboundProxyDiagnostic{
			Kind: "invalid", Origin: origin,
			Message: fmt.Sprintf("%s uses the unsupported %s:// scheme; connecting directly for that scheme — set an http:// or https:// proxy URL instead", origin, strings.TrimSuffix(parsed.Scheme, ":")),
		})
	}
	return proxyCandidate{kind: proxyRejected}
}

func proxyCandidateURL(candidate proxyCandidate, fallbacks ...*url.URL) *url.URL {
	switch candidate.kind {
	case proxyAccepted:
		return candidate.value
	case proxyRejected:
		return nil
	default:
		for _, fallback := range fallbacks {
			if fallback != nil {
				return fallback
			}
		}
		return nil
	}
}

// ResolveOutboundProxy applies the DSH process/user launch-environment policy.
// Project .env proxy settings are rejected while $DSH_HOME/.env is a supported
// source. Lowercase spellings win over uppercase spellings; blank is absent.
func ResolveOutboundProxy(snapshot *LaunchEnvironmentSnapshot) OutboundProxyResolution {
	resolution := OutboundProxyResolution{}
	all := acceptProxyCandidate(snapshot, "all_proxy", &resolution.Diagnostics)
	var allURL *url.URL
	if all.kind == proxyAccepted {
		allURL = all.value
	}
	httpCandidate := acceptProxyCandidate(snapshot, "http_proxy", &resolution.Diagnostics)
	httpsCandidate := acceptProxyCandidate(snapshot, "https_proxy", &resolution.Diagnostics)
	httpProxy := proxyCandidateURL(httpCandidate, allURL)
	httpsProxy := proxyCandidateURL(httpsCandidate, allURL, httpProxy)
	if httpProxy == nil && httpsProxy == nil {
		return resolution
	}
	noProxyEntry, _, _ := proxyEnvironmentEntry(snapshot, "no_proxy")
	resolution.Policy = OutboundProxyPolicy{
		httpProxy: httpProxy, httpsProxy: httpsProxy,
		noProxy: mergeLoopbackNoProxy(noProxyEntry.Value), source: true,
		inherited: snapshotProxyEnvironment(snapshot),
	}
	return resolution
}

func snapshotProxyEnvironment(snapshot *LaunchEnvironmentSnapshot) map[string]*string {
	values := make(map[string]*string, len(outboundProxyEnvironmentNames))
	for _, name := range outboundProxyEnvironmentNames {
		entry, found := snapshot.GetFrom(name, LaunchEnvironmentProcess, LaunchEnvironmentUser)
		if !found {
			values[name] = nil
			continue
		}
		value := entry.Value
		values[name] = &value
	}
	return values
}

func mergeLoopbackNoProxy(value string) string {
	entries := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	for _, entry := range entries {
		if entry == "*" {
			return "*"
		}
	}
	present := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		present[strings.ToLower(entry)] = struct{}{}
	}
	for _, entry := range loopbackNoProxy {
		if _, found := present[strings.ToLower(entry)]; !found {
			entries = append(entries, entry)
		}
	}
	return strings.Join(entries, ",")
}

func splitProxyHostPort(entry string) (string, string) {
	if strings.HasPrefix(entry, "[") {
		if close := strings.IndexByte(entry, ']'); close >= 0 {
			host, rest := entry[1:close], entry[close+1:]
			if strings.HasPrefix(rest, ":") {
				return host, rest[1:]
			}
			return host, ""
		}
	}
	if strings.Count(entry, ":") == 1 {
		host, port, _ := strings.Cut(entry, ":")
		return host, port
	}
	return entry, ""
}

func isLoopbackProxyHost(hostname string) bool {
	host := strings.TrimSuffix(strings.Trim(strings.ToLower(hostname), "[]"), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	address = address.Unmap()
	return address.IsLoopback() || address.IsUnspecified()
}

func bypassesOutboundProxy(noProxy string, target *url.URL) bool {
	host := strings.TrimSuffix(strings.Trim(strings.ToLower(target.Hostname()), "[]"), ".")
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	for _, raw := range strings.FieldsFunc(noProxy, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
		entry := strings.ToLower(strings.TrimSpace(raw))
		if entry == "*" {
			return true
		}
		candidate, candidatePort := splitProxyHostPort(entry)
		if candidatePort != "" && candidatePort != port {
			continue
		}
		candidate = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(candidate, "*."), "."), ".")
		if candidate != "" && (host == candidate || strings.HasSuffix(host, "."+candidate)) {
			return true
		}
	}
	return false
}

// Proxy is compatible with http.Transport.Proxy. It always keeps loopback and
// unspecified destinations direct, including all of 127/8 and IPv4-mapped
// spellings.
func (p OutboundProxyPolicy) Proxy(request *http.Request) (*url.URL, error) {
	if request == nil || request.URL == nil || !p.source || isLoopbackProxyHost(request.URL.Hostname()) || bypassesOutboundProxy(p.noProxy, request.URL) {
		return nil, nil
	}
	switch strings.ToLower(request.URL.Scheme) {
	case "http":
		return p.httpProxy, nil
	case "https":
		return p.httpsProxy, nil
	default:
		return nil, nil
	}
}

var (
	outboundProxyInstallMu     sync.Mutex
	standardOutboundTransport  = http.DefaultTransport.(*http.Transport).Clone()
	outboundProxyInstallations = make(map[*http.Transport]*outboundProxyInstallation)
)

type outboundProxyInstallation struct {
	previous  http.RoundTripper
	installed *http.Transport
	disposed  bool
}

// InstallOutboundProxy replaces Go's process-wide default transport with a
// transport governed by policy, including when policy is direct. The latter is
// important because http.ProxyFromEnvironment has different fallback rules.
// The returned disposer restores the previous transport.
func InstallOutboundProxy(policy OutboundProxyPolicy) func() {
	outboundProxyInstallMu.Lock()
	previous := http.DefaultTransport
	base, ok := previous.(*http.Transport)
	if !ok {
		base = standardOutboundTransport
	}
	installed := base.Clone()
	installed.Proxy = policy.Proxy
	installation := &outboundProxyInstallation{previous: previous, installed: installed}
	outboundProxyInstallations[installed] = installation
	http.DefaultTransport = installed
	outboundProxyInstallMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			outboundProxyInstallMu.Lock()
			installed.CloseIdleConnections()
			installation.disposed = true
			if http.DefaultTransport == installed {
				current := installation
				for current.disposed {
					http.DefaultTransport = current.previous
					delete(outboundProxyInstallations, current.installed)
					previousTransport, ok := current.previous.(*http.Transport)
					if !ok {
						break
					}
					previousInstallation, found := outboundProxyInstallations[previousTransport]
					if !found {
						break
					}
					current = previousInstallation
				}
			}
			outboundProxyInstallMu.Unlock()
		})
	}
}

// ProxyForURL resolves a URL without constructing a complete caller-owned
// transport. It is useful for security-sensitive providers that branch between
// direct DNS pinning and proxy-side name resolution.
func (p OutboundProxyPolicy) ProxyForURL(target *url.URL) (*url.URL, bool) {
	proxy, _ := p.Proxy(&http.Request{URL: target})
	return proxy, proxy != nil
}

func supportedProxyEnvironmentURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https"))
}

// ChildEnvironment returns DSH's normalized subprocess environment. Values a
// user explicitly named keep their original casing and value (so tools such as
// curl may still use SOCKS); absent scheme slots receive the resolved fallback.
// NODE_USE_ENV_PROXY is withheld if Node would reject a preserved value.
func (p OutboundProxyPolicy) ChildEnvironment(base []string) []string {
	if !p.source || p.inherited == nil {
		return append([]string(nil), base...)
	}
	overlay := map[string]*string{}
	nodeProxy := "1"
	overlay["NODE_USE_ENV_PROXY"] = &nodeProxy
	for _, slot := range []struct {
		names    [2]string
		resolved *url.URL
	}{
		{names: [2]string{"http_proxy", "HTTP_PROXY"}, resolved: p.httpProxy},
		{names: [2]string{"https_proxy", "HTTPS_PROXY"}, resolved: p.httpsProxy},
	} {
		named := p.inherited[slot.names[0]] != nil || p.inherited[slot.names[1]] != nil
		for _, name := range slot.names {
			if named {
				overlay[name] = p.inherited[name]
			} else if slot.resolved != nil {
				value := slot.resolved.String()
				overlay[name] = &value
			} else {
				overlay[name] = nil
			}
		}
	}
	for _, name := range [...]string{"no_proxy", "NO_PROXY"} {
		value := p.noProxy
		overlay[name] = &value
	}
	for _, name := range [...]string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY"} {
		if value := overlay[name]; value != nil && !supportedProxyEnvironmentURL(*value) {
			overlay["NODE_USE_ENV_PROXY"] = nil
			break
		}
	}
	return applyEnvironmentOverlay(base, overlay)
}

func applyEnvironmentOverlay(base []string, overlay map[string]*string) []string {
	key := func(name string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(name)
		}
		return name
	}
	remaining := make(map[string]*string, len(overlay))
	for name, value := range overlay {
		remaining[key(name)] = value
	}
	result := make([]string, 0, len(base)+len(overlay))
	for _, entry := range base {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, replaced := remaining[key(name)]; replaced {
			continue
		}
		result = append(result, entry)
	}
	for _, name := range [...]string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY", "no_proxy", "NO_PROXY", "NODE_USE_ENV_PROXY"} {
		lookup := key(name)
		value, found := remaining[lookup]
		if !found {
			continue
		}
		delete(remaining, lookup)
		if value != nil {
			result = append(result, name+"="+*value)
		}
	}
	return result
}
