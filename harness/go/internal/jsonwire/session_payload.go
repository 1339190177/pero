package jsonwire

import (
	"encoding/json"
	"strings"
)

// ValidateSessionPayload applies, in order, the three checks a Session read
// owes its trusted subtrees: UTF-8, JavaScript-safe numbers, DSH snapshot
// losslessness. The three token walks are expensive on a long history, so a
// single scan first accepts payloads in which every number is provably fine.
// Whenever that scan has any doubt the original validators decide, so errors
// and verdicts are exactly theirs; the scan can only make acceptance cheaper.
func ValidateSessionPayload(data []byte) error {
	if err := ValidateJSONUTF8(data); err != nil {
		return err
	}
	if json.Valid(data) && numbersAreSafe(data) {
		return nil
	}
	if err := ValidateJavaScriptJSONNumbers(data); err != nil {
		return err
	}
	return ValidateDSHJSONSnapshot(data)
}

// numbersAreSafe reports true only when every number literal in the valid JSON
// document round-trips through float64 and is not a negative zero.
func numbersAreSafe(data []byte) bool {
	for index := 0; index < len(data); {
		switch c := data[index]; {
		case c == '"':
			for index++; index < len(data) && data[index] != '"'; index++ {
				if data[index] == '\\' {
					index++
				}
			}
			index++
		case c == '-' || c >= '0' && c <= '9':
			end := index + 1
			plain := true
			for ; end < len(data); end++ {
				d := data[end]
				if d >= '0' && d <= '9' {
					continue
				}
				if d == '.' || d == 'e' || d == 'E' || d == '+' || d == '-' {
					plain = false
					continue
				}
				break
			}
			if !numberIsSafe(data[index:end], plain) {
				return false
			}
			index = end
		default:
			index++
		}
	}
	return true
}

func numberIsSafe(literal []byte, plainInteger bool) bool {
	digits := len(literal)
	if literal[0] == '-' {
		digits--
	}
	// At most 15 digits is exactly representable and prints back canonically.
	if plainInteger && digits <= 15 {
		return !(literal[0] == '-' && digits == 1 && literal[1] == '0')
	}
	text := string(literal)
	if text[0] == '-' {
		mantissa := text[1:]
		if cut := strings.IndexAny(mantissa, "eE"); cut >= 0 {
			mantissa = mantissa[:cut]
		}
		if strings.Trim(mantissa, "0.") == "" {
			return false
		}
	}
	return numberRoundTrips(text)
}

// NumbersAreSafe reports whether every number in a valid JSON document round-
// trips through float64 and none is a negative zero: the condition under which a
// JavaScript consumer reads the document without changing a value.
func NumbersAreSafe(data []byte) bool { return numbersAreSafe(data) }
