package jsonwire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const UnrepresentableJSONNumberCode = "UNREPRESENTABLE_JSON_NUMBER"

// JSONNumberError reports only an anonymized, bounded field path and type.
// It deliberately omits the rejected numeric literal and opaque key names.
type JSONNumberError struct{ Path string }

func (e *JSONNumberError) Error() string {
	return UnrepresentableJSONNumberCode + ": JSON number at " + e.Path + " cannot round-trip through the JavaScript boundary"
}

// decimalIdentity uses only bounded integer exponent arithmetic, not powers of
// ten or a big.Rat. In particular a tiny input such as 0e999999999999 cannot
// force an enormous allocation while being validated.
func decimalIdentity(value string) (string, int64, bool) {
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	mantissa, exponent := value, "0"
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		mantissa, exponent = value[:index], value[index+1:]
	}
	fraction := int64(0)
	if index := strings.IndexByte(mantissa, '.'); index >= 0 {
		fraction = int64(len(mantissa) - index - 1)
		mantissa = mantissa[:index] + mantissa[index+1:]
	}
	digits := strings.TrimLeft(mantissa, "0")
	if digits == "" {
		return "0", 0, true
	}
	exp, err := strconv.ParseInt(exponent, 10, 64)
	if err != nil {
		return "", 0, false
	}
	// Finite nonzero float64 values need only a few hundred decimal places.
	// Bound arithmetic before adding input length so adversarial exponents
	// cannot wrap. The input itself already bounds all digit allocations.
	bound := int64(len(value)) + 1000
	if exp < -bound || exp > bound {
		return "", 0, false
	}
	trimmed := strings.TrimRight(digits, "0")
	exp = exp - fraction + int64(len(digits)-len(trimmed))
	if negative {
		trimmed = "-" + trimmed
	}
	return trimmed, exp, true
}

func numberRoundTrips(value string) bool {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return false
	}
	digits, exp, ok := decimalIdentity(value)
	if !ok {
		return false
	}
	renderedDigits, renderedExp, renderedOK := decimalIdentity(strconv.FormatFloat(number, 'g', -1, 64))
	return renderedOK && digits == renderedDigits && exp == renderedExp
}

func (e *JSONNumberError) ToolErrorCode() string { return UnrepresentableJSONNumberCode }

func boundedJSONPath(path string) string {
	if len(path) > 240 {
		return path[:237] + "..."
	}
	return path
}
func jsonFieldPath(path, key string) string {
	// Only fixed public protocol names can appear in diagnostics.
	switch key {
	case "request", "messages", "content", "events", "data", "message", "tools", "extensions", "seq", "sourceEventSeqs", "turn", "step", "revision", "expectedRevision", "createdAtMs", "header", "config", "value", "entries", "profile", "bootstrap":
		return boundedJSONPath(path + "." + key)
	default:
		return boundedJSONPath(path + ".<field>")
	}
}

// ValidateJavaScriptJSONNumbers is a stricter Point transport boundary: native
// raw JSON storage can retain arbitrary decimal numbers, but a Node consumer
// must never silently change its decimal value at the boundary. Equivalent
// decimal spellings such as 1.0/1e0 are accepted. The helper itself permits
// zero-sign normalization; content/Session callers also use the snapshot check.
// It is never installed as a generic Config, bootstrap or transport validator.
// No numeric payload is logged.
func ValidateJavaScriptJSONNumbers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var visit func(string) error
	visit = func(path string) error {
		token, err := decoder.Token()
		if err != nil {
			return errors.New("bridge payload is not valid JSON")
		}
		switch value := token.(type) {
		case json.Number:
			if !numberRoundTrips(string(value)) {
				return &JSONNumberError{Path: boundedJSONPath(path)}
			}
		case json.Delim:
			if value == '{' {
				for decoder.More() {
					key, err := decoder.Token()
					if err != nil {
						return errors.New("bridge payload is not valid JSON")
					}
					name, ok := key.(string)
					if !ok {
						return errors.New("bridge payload is not valid JSON")
					}
					if err := visit(jsonFieldPath(path, name)); err != nil {
						return err
					}
				}
			} else if value == '[' {
				index := 0
				for decoder.More() {
					if err := visit(boundedJSONPath(fmt.Sprintf("%s[%d]", path, index))); err != nil {
						return err
					}
					index++
				}
			} else {
				return errors.New("bridge payload is not valid JSON")
			}
			if _, err := decoder.Token(); err != nil {
				return errors.New("bridge payload is not valid JSON")
			}
		}
		return nil
	}
	if err := visit("$"); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("bridge payload must contain exactly one JSON value")
	}
	return nil
}
