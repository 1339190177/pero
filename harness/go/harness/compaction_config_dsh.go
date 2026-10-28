package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	harnesstool "github.com/phodal/pero/harness/go/tool"
)

const (
	DSHCompactionSourceInvalid     = "DSH_COMPACTION_SOURCE_INVALID"
	DSHCompactionNativeUnsupported = "DSH_COMPACTION_NATIVE_UNSUPPORTED"
)

// DSHCompactionConfigError distinguishes the source configuration grammar from
// a value the native execution representation cannot carry. In particular,
// JavaScript Number.isInteger accepts integers outside Go's native int range.
// Diagnostics contain only fixed field paths, never configured target names.
type DSHCompactionConfigError struct {
	Code    string
	Path    string
	Message string
}

func (e *DSHCompactionConfigError) Error() string {
	return e.Code + ": " + e.Path + ": " + e.Message
}

func dshCompactionInvalid(path, message string) error {
	return &DSHCompactionConfigError{DSHCompactionSourceInvalid, path, message}
}

func dshCompactionUnsupported(path, message string) error {
	return &DSHCompactionConfigError{DSHCompactionNativeUnsupported, path, message}
}

type dshCompactionPolicy struct {
	provider, model                           string
	threshold, headroom, retainRatio, retain  *float64
	maxTokens, retries, overflowRetries       *float64
	summarizationProvider, summarizationModel *string
}

// DecodeDSHBasicCompactionConfig applies the pinned alpha.3 Config schema's
// normalization and resolveConfig semantics, then narrows to native execution.
// Omission and top-level null select defaults; modelPolicies:null selects an
// empty list. Other explicit null fields are invalid. Legal JavaScript integer
// values are validated as Numbers before any native int conversion occurs.
// This decoder does not activate an owner or change any existing Agent.
func DecodeDSHBasicCompactionConfig(raw json.RawMessage) (BasicCompactionConfig, error) {
	var zero BasicCompactionConfig
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) {
		return zero, dshCompactionInvalid("config", "requires one JSON value")
	}
	if !utf8.Valid(raw) {
		return zero, dshCompactionInvalid("config", "JSON text must be valid UTF-8")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return zero, dshCompactionInvalid("config", "invalid JSON value")
	}
	if value == nil {
		value = map[string]any{}
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return zero, dshCompactionInvalid("config", "must be an object")
	}
	base, err := parseDSHCompactionPolicy(fields, "config", false)
	if err != nil {
		return zero, err
	}
	auto := true
	if value, present := fields["auto"]; present {
		if auto, ok = value.(bool); !ok {
			return zero, dshCompactionInvalid("config.auto", "must be a boolean")
		}
	}
	rootMaxExplicit := base.maxTokens != nil
	base.threshold = dshDefaultNumber(base.threshold, .8)
	base.headroom = dshDefaultNumber(base.headroom, 65536)
	base.maxTokens = dshDefaultNumber(base.maxTokens, *base.headroom)
	base.retries = dshDefaultNumber(base.retries, 1)
	base.overflowRetries = dshDefaultNumber(base.overflowRetries, 1)
	if base.retain == nil && base.retainRatio == nil {
		base.retainRatio = dshDefaultNumber(nil, .16)
	}
	if base.summarizationProvider == nil {
		empty := ""
		base.summarizationProvider, base.summarizationModel = &empty, &empty
	}
	if *base.maxTokens <= 0 {
		return zero, dshCompactionInvalid("config.maxTokens", "explicit or inherited maxTokens must be positive")
	}
	if err := validateDSHCompactionRetention(base, base, "config"); err != nil {
		return zero, err
	}
	var policies []dshCompactionPolicy
	if value := fields["modelPolicies"]; value != nil {
		array, ok := value.([]any)
		if !ok {
			return zero, dshCompactionInvalid("config.modelPolicies", "must be an array")
		}
		// Retain original string tokens for exact UTF-16 identity. Go's normal
		// JSON string view is sufficient for type/emptiness validation, but it
		// replaces lone surrogates and must never drive duplicate detection.
		var rawFields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &rawFields)
		var rawPolicies []json.RawMessage
		_ = json.Unmarshal(rawFields["modelPolicies"], &rawPolicies)
		seen := map[string]struct{}{}
		for index, value := range array {
			path := fmt.Sprintf("config.modelPolicies[%d]", index)
			fields, ok := value.(map[string]any)
			if !ok {
				return zero, dshCompactionInvalid(path, "must be an object")
			}
			policy, err := parseDSHCompactionPolicy(fields, path, true)
			if err != nil {
				return zero, err
			}
			var original map[string]json.RawMessage
			_ = json.Unmarshal(rawPolicies[index], &original)
			key := dshCompactionUTF16Identity(original["provider"]) + "\x00\x00" + dshCompactionUTF16Identity(original["model"])
			if _, duplicate := seen[key]; duplicate {
				return zero, dshCompactionInvalid(path, "duplicate exact model policy")
			}
			seen[key] = struct{}{}
			if policy.maxTokens == nil && !rootMaxExplicit && policy.headroom != nil {
				policy.maxTokens = cloneFloat(policy.headroom)
			}
			if policy.maxTokens != nil && *policy.maxTokens <= 0 {
				return zero, dshCompactionInvalid(path+".maxTokens", "explicit or inherited maxTokens must be positive")
			}
			if err := validateDSHCompactionRetention(base, policy, path); err != nil {
				return zero, err
			}
			policies = append(policies, policy)
		}
	}
	// Native string decoding is a narrowing boundary too. Its rejection must
	// follow the source grammar, not turn an unknown key or duplicate target
	// into a misleading native-representation failure.
	if err := harnesstool.ValidateNativeJSONDecoding(raw); err != nil {
		return zero, dshCompactionUnsupported("config", "native string decoding cannot preserve this source-valid JSON value")
	}
	// Every source validation above precedes narrowing. A huge source-valid
	// number must not hide a later genuinely invalid policy as an int overflow.
	config, err := nativeDSHCompactionConfig(base, policies, auto)
	if err != nil {
		return zero, err
	}
	if _, err := resolveBasicCompactionConfig(config); err != nil {
		return zero, dshCompactionUnsupported("config", "source-valid policy is not supported by the native compactor")
	}
	return config, nil
}

func parseDSHCompactionPolicy(fields map[string]any, path string, target bool) (dshCompactionPolicy, error) {
	var policy dshCompactionPolicy
	for key := range fields {
		switch key {
		case "thresholdRatio", "headroomTokens", "retainRatio", "retainTokens", "summarizationProvider", "summarizationModel", "maxTokens", "compactionRetries", "maxOverflowRetries":
		case "auto", "modelPolicies":
			if target {
				return policy, dshCompactionInvalid(path, "unknown model-policy field")
			}
		case "provider", "model":
			if !target {
				return policy, dshCompactionInvalid(path, "unknown root field")
			}
		default:
			return policy, dshCompactionInvalid(path, "unknown field")
		}
	}
	for _, field := range []struct {
		name string
		to   **float64
		kind string
	}{
		{"thresholdRatio", &policy.threshold, "ratio"}, {"headroomTokens", &policy.headroom, "nonnegative"},
		{"retainRatio", &policy.retainRatio, "ratio"}, {"retainTokens", &policy.retain, "nonnegative"},
		{"maxTokens", &policy.maxTokens, "positive"}, {"compactionRetries", &policy.retries, "nonnegative"},
		{"maxOverflowRetries", &policy.overflowRetries, "nonnegative"},
	} {
		value, present := fields[field.name]
		if !present {
			continue
		}
		number, ok := value.(json.Number)
		if !ok {
			return policy, dshCompactionInvalid(path+"."+field.name, "must be a number")
		}
		n, err := strconv.ParseFloat(string(number), 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return policy, dshCompactionInvalid(path+"."+field.name, "must be a finite number")
		}
		valid := n >= 0 && math.Trunc(n) == n
		if field.kind == "ratio" {
			valid = n > 0 && n <= 1
		} else if field.kind == "positive" {
			valid = valid && n > 0
		}
		if !valid {
			return policy, dshCompactionInvalid(path+"."+field.name, "number is outside the source field domain")
		}
		*field.to = &n
	}
	if policy.retain != nil && policy.retainRatio != nil {
		return policy, dshCompactionInvalid(path, "retainRatio and retainTokens are mutually exclusive")
	}
	for _, field := range []struct {
		name string
		to   **string
	}{
		{"summarizationProvider", &policy.summarizationProvider}, {"summarizationModel", &policy.summarizationModel},
	} {
		if value, present := fields[field.name]; present {
			text, ok := value.(string)
			if !ok {
				return policy, dshCompactionInvalid(path+"."+field.name, "must be a string")
			}
			*field.to = &text
		}
	}
	if (policy.summarizationProvider == nil) != (policy.summarizationModel == nil) || policy.summarizationProvider != nil && ((*policy.summarizationProvider == "") != (*policy.summarizationModel == "")) {
		return policy, dshCompactionInvalid(path, "summarization target must be an omitted, empty, or non-empty pair")
	}
	if target {
		var ok bool
		policy.provider, ok = fields["provider"].(string)
		if !ok || policy.provider == "" {
			return policy, dshCompactionInvalid(path+".provider", "must be a non-empty string")
		}
		policy.model, ok = fields["model"].(string)
		if !ok || policy.model == "" {
			return policy, dshCompactionInvalid(path+".model", "must be a non-empty string")
		}
	}
	return policy, nil
}

func dshDefaultNumber(value *float64, fallback float64) *float64 {
	if value != nil {
		return cloneFloat(value)
	}
	return &fallback
}

func validateDSHCompactionRetention(base, override dshCompactionPolicy, path string) error {
	threshold := base.threshold
	if override.threshold != nil {
		threshold = override.threshold
	}
	ratio := base.retainRatio
	if override.retain != nil {
		ratio = nil
	} else if override.retainRatio != nil {
		ratio = override.retainRatio
	}
	if ratio != nil && *ratio >= *threshold {
		return dshCompactionInvalid(path, "resolved retainRatio must be below thresholdRatio")
	}
	return nil
}

func nativeDSHCompactionConfig(base dshCompactionPolicy, policies []dshCompactionPolicy, auto bool) (BasicCompactionConfig, error) {
	var conversionErr error
	integer := func(value *float64, path string) *int {
		if value == nil || conversionErr != nil {
			return nil
		}
		// float64(MaxInt) rounds up on 64-bit hosts. Use the exclusive power
		// of two bound so conversion cannot wrap at that rounded boundary.
		if *value >= math.Ldexp(1, strconv.IntSize-1) {
			conversionErr = dshCompactionUnsupported(path, "source-valid integer exceeds native execution range")
			return nil
		}
		v := int(*value)
		return &v
	}
	root := ModelCompactionPolicy{
		HeadroomTokens: integer(base.headroom, "config.headroomTokens"), RetainTokens: integer(base.retain, "config.retainTokens"),
		MaxTokens: integer(base.maxTokens, "config.maxTokens"), CompactionRetries: integer(base.retries, "config.compactionRetries"),
		MaxOverflowRetries: integer(base.overflowRetries, "config.maxOverflowRetries"),
	}
	if conversionErr != nil {
		return BasicCompactionConfig{}, conversionErr
	}
	config := BasicCompactionConfig{
		dshSourcePolicy: true, ThresholdRatio: *base.threshold, HeadroomTokens: root.HeadroomTokens,
		RetainRatio: cloneFloat(base.retainRatio), RetainTokens: root.RetainTokens,
		SummarizationProvider: *base.summarizationProvider, SummarizationModel: *base.summarizationModel,
		MaxTokens: *root.MaxTokens, CompactionRetries: root.CompactionRetries, MaxOverflowRetries: root.MaxOverflowRetries,
		Auto: &auto, ModelPolicies: make([]ModelCompactionPolicy, 0, len(policies)),
	}
	for index, policy := range policies {
		path := fmt.Sprintf("config.modelPolicies[%d]", index)
		config.ModelPolicies = append(config.ModelPolicies, ModelCompactionPolicy{
			Provider: policy.provider, Model: policy.model, ThresholdRatio: cloneFloat(policy.threshold),
			HeadroomTokens: integer(policy.headroom, path+".headroomTokens"), RetainRatio: cloneFloat(policy.retainRatio),
			RetainTokens: integer(policy.retain, path+".retainTokens"), MaxTokens: integer(policy.maxTokens, path+".maxTokens"),
			CompactionRetries: integer(policy.retries, path+".compactionRetries"), MaxOverflowRetries: integer(policy.overflowRetries, path+".maxOverflowRetries"),
			SummarizationProvider: cloneString(policy.summarizationProvider), SummarizationModel: cloneString(policy.summarizationModel),
		})
	}
	if conversionErr != nil {
		return BasicCompactionConfig{}, conversionErr
	}
	return config, nil
}

// dshCompactionUTF16Identity is used only after JSON validity and the source
// string type check. Each two-byte unit preserves JavaScript string identity,
// including lone surrogate escapes; a valid literal scalar and its escaped
// surrogate pair produce the same key. This never becomes a native target.
func dshCompactionUTF16Identity(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	data := raw[1 : len(raw)-1]
	var result strings.Builder
	unit := func(value uint16) {
		result.WriteByte(byte(value >> 8))
		result.WriteByte(byte(value))
	}
	for len(data) != 0 {
		if data[0] == '\\' {
			escaped := data[1]
			if escaped == 'u' {
				value, _ := strconv.ParseUint(string(data[2:6]), 16, 16)
				unit(uint16(value))
				data = data[6:]
				continue
			}
			switch escaped {
			case 'b':
				escaped = '\b'
			case 'f':
				escaped = '\f'
			case 'n':
				escaped = '\n'
			case 'r':
				escaped = '\r'
			case 't':
				escaped = '\t'
			}
			unit(uint16(escaped))
			data = data[2:]
			continue
		}
		value, size := utf8.DecodeRune(data)
		if value <= 0xffff {
			unit(uint16(value))
		} else {
			high, low := utf16.EncodeRune(value)
			unit(uint16(high))
			unit(uint16(low))
		}
		data = data[size:]
	}
	return result.String()
}
