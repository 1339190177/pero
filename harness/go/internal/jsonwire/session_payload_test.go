package jsonwire

import (
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

// The accelerated validator must agree with the original three, in order.
func slowSessionValidation(data []byte) error {
	if err := ValidateJSONUTF8(data); err != nil {
		return err
	}
	if err := ValidateJavaScriptJSONNumbers(data); err != nil {
		return err
	}
	return ValidateDSHJSONSnapshot(data)
}

func sameVerdict(t *testing.T, payload string) {
	t.Helper()
	fast, slow := ValidateSessionPayload([]byte(payload)), slowSessionValidation([]byte(payload))
	if (fast == nil) != (slow == nil) || fast != nil && fast.Error() != slow.Error() {
		t.Fatalf("payload %q: fast=%v slow=%v", payload, fast, slow)
	}
	var fastNumber, slowNumber *JSONNumberError
	if errors.As(fast, &fastNumber) != errors.As(slow, &slowNumber) || fastNumber != nil && *fastNumber != *slowNumber {
		t.Fatalf("payload %q: number error path differs: %v vs %v", payload, fast, slow)
	}
}

func TestSessionPayloadVerdictsMatchTheOriginalValidators(t *testing.T) {
	for _, payload := range []string{
		`{"a":1,"b":[0,-1,12345678901234,0.5,-0.25,1e3,1E-3,2.50]}`,
		`{"s":"digits 123 \"-0\" and \\ -0.0 inside strings","n":7}`,
		`{"neg":-0}`, `{"neg":-0.0}`, `[-0e5]`, `[-0.000]`, `[0,-1e0]`,
		`{"big":9007199254740993}`, `{"big":123456789012345678}`, `{"f":0.1}`, `{"f":1e400}`, `{"f":1e-400}`,
		`{"path":{"events":[{"data":{"x":1.5,"y":123456789012345678901}}]}}`,
		`1`, `-5`, `"just a string"`, `null`, `[]`, `{}`,
		`{"a":1} trailing`, `{"a":`, `not json`, ``, "\"bad\xffutf8\"",
	} {
		sameVerdict(t, payload)
	}
}

func TestSessionPayloadIntegerShortcutAgreesWithRoundTrip(t *testing.T) {
	values := []int64{0, 1, 9, 10, 100, 1000000, 999999999999999, 100000000000000, -1, -10, -999999999999999, -100000000000000}
	random := rand.New(rand.NewSource(7))
	for i := 0; i < 20000; i++ {
		digits := 1 + random.Intn(15)
		value := random.Int63n(int64(pow10(digits)))
		if random.Intn(2) == 0 {
			value = -value
		}
		if random.Intn(4) == 0 {
			value -= value % 1000
		}
		values = append(values, value)
	}
	for _, value := range values {
		literal := strconv.FormatInt(value, 10)
		if literal == "0" || literal == "-0" {
			continue
		}
		if !numberRoundTrips(literal) {
			t.Fatalf("%s is a short integer but does not round-trip", literal)
		}
	}
}

func pow10(n int) float64 {
	result := 1.0
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}

func TestSessionPayloadRandomDocumentsMatch(t *testing.T) {
	random := rand.New(rand.NewSource(11))
	atoms := []string{"0", "-0", "1", "-7", "3.14", "-0.0", "1e2", "2E-3", "1e400", "0.1", "123456789012345678", "999999999999999", "1234567890123456", `"x -0 y"`, `"q\"1\""`, "true", "null"}
	for i := 0; i < 3000; i++ {
		var parts []string
		for j := 0; j < 1+random.Intn(8); j++ {
			parts = append(parts, fmt.Sprintf(`"k%d":%s`, j, atoms[random.Intn(len(atoms))]))
		}
		sameVerdict(t, "{"+strings.Join(parts, ",")+`,"arr":[`+atoms[random.Intn(len(atoms))]+","+atoms[random.Intn(len(atoms))]+"]}")
	}
}

