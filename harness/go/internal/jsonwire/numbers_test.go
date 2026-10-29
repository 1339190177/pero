package jsonwire

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestJavaScriptNumbersRejectPrecisionLossWithoutLeakingValues(t *testing.T) {
	for _, raw := range []string{`{"value":0.1}`, `{"value":1.0}`, `{"value":1e0}`, `{"value":-0}`, `{"value":0e999999999999999999999999}`, `{"value":-0.000e-999999999999999999999}`, `{"value":9007199254740992}`, `{"value":"9007199254740993"}`, `{"createdAtMs":1791615000000}`, `{"data":{"nanoseconds":"1791615000000000001"}}`} {
		if err := ValidateJavaScriptJSONNumbers([]byte(raw)); err != nil {
			t.Fatalf("roundtrippable value rejected: %s %v", raw, err)
		}
	}
	for _, raw := range []string{`{"events":[{"data":{"secret-name":9007199254740993}}]}`, `{"request":{"messages":[{"content":[{"type":"widget","value":0.10000000000000001}]}]}}`, `{"value":1e9999}`, `{"value":1e-9999}`} {
		err := ValidateJavaScriptJSONNumbers([]byte(raw))
		var number *JSONNumberError
		if !errors.As(err, &number) || !strings.Contains(err.Error(), UnrepresentableJSONNumberCode) {
			t.Fatalf("missing typed precision failure: %v", err)
		}
		if strings.Contains(err.Error(), "secret-name") || strings.Contains(err.Error(), "9007199254740993") || len(number.Path) > 240 {
			t.Fatalf("unsafe diagnostic: %v", err)
		}
	}
}

func TestDSHSnapshotIsExplicitAndPreservesNativeBytes(t *testing.T) {
	for _, raw := range []string{`-0`, `-0.0e900000`, `{"private-field":[-0]}`} {
		if !errors.Is(ValidateDSHJSONSnapshot([]byte(raw)), ErrNonLosslessJSON) {
			t.Fatalf("signed zero accepted")
		}
		if err := ValidateJavaScriptJSONNumbers([]byte(raw)); err != nil {
			t.Fatalf("number-only contract changed: %v", err)
		}
	}
	for _, raw := range []string{`9007199254740993`, `0.10000000000000001`, `{"value":"\ud800"}`} {
		if err := ValidateDSHJSONSnapshot([]byte(raw)); err != nil {
			t.Fatalf("snapshot helper rounded/decoded raw data: %v", err)
		}
	}
}

type changingJSON struct{ calls int }

func (value *changingJSON) MarshalJSON() ([]byte, error) {
	value.calls++
	if value.calls > 1 {
		return []byte(`{"content":[9007199254740993]}`), nil
	}
	return []byte(`{"content":[1]}`), nil
}
func TestFreezeValidatesAndReturnsTheOneStatefulEncoding(t *testing.T) {
	value := &changingJSON{}
	var checked []byte
	frozen, err := Freeze(value, func(data []byte) error {
		checked = append([]byte(nil), data...)
		return ValidateJavaScriptJSONNumbers(data)
	})
	if err != nil || value.calls != 1 || !bytes.Equal(frozen, checked) {
		t.Fatalf("freeze calls=%d err=%v", value.calls, err)
	}
	encoded, err := json.Marshal(frozen)
	if err != nil || value.calls != 1 || !bytes.Equal(encoded, checked) {
		t.Fatalf("frozen transport changed bytes or reevaluated producer")
	}
}

type nativeJSONBothEncoders struct {
	Calls  *int
	Hidden string
}

func (v nativeJSONBothEncoders) MarshalJSON() ([]byte, error) {
	*v.Calls++
	return []byte(`{"safe":true}`), nil
}
func (nativeJSONBothEncoders) MarshalText() ([]byte, error) { panic("JSON must precede Text") }

type nativeJSONTextOnly struct{}

func (nativeJSONTextOnly) MarshalText() ([]byte, error) { panic("preflight must not invoke Text") }

type nativeJSONTextInteger int

func (nativeJSONTextInteger) MarshalText() ([]byte, error) {
	panic("integer Text key must reject without invocation")
}

type nativeJSONTextString string

func (nativeJSONTextString) MarshalText() ([]byte, error) {
	panic("string map keys must not invoke Text")
}

type nativeJSONTextByte byte

func (nativeJSONTextByte) MarshalText() ([]byte, error) {
	panic("custom byte elements are not base64 bytes")
}

type nativeJSONCustomZero struct{ Text string }

func (nativeJSONCustomZero) IsZero() bool { panic("preflight must not call IsZero") }

type nativeJSONEmbedA struct{ Value string }
type nativeJSONEmbedB struct{ Value string }
type nativeJSONEmbedTagged struct {
	Value string `json:"Value"`
}

func TestNativeJSONStringsChecksOnlyEncodedNativeFieldsWithoutMethods(t *testing.T) {
	bad := string([]byte{'x', 0xff})
	calls := 0
	both := nativeJSONBothEncoders{Calls: &calls, Hidden: bad}
	accepted := []any{
		"valid replacement \ufffd and Unicode 世界", []byte{0xff}, json.RawMessage{'"', 0xff, '"'},
		both, map[nativeJSONTextString]string{"safe": "safe"},
		(*nativeJSONBothEncoders)(nil), (*nativeJSONTextOnly)(nil),
		map[*nativeJSONTextOnly]int{nil: 1},
		struct {
			Count int64 `json:"count,string"`
		}{9007199254740993},
		struct {
			Hidden  string `json:"-"`
			hidden  string
			Visible string
		}{bad, bad, "safe"},
		struct {
			nativeJSONEmbedA
			Value string
		}{nativeJSONEmbedA{bad}, "safe"},
		struct {
			nativeJSONEmbedA
			nativeJSONEmbedB
		}{nativeJSONEmbedA{bad}, nativeJSONEmbedB{bad}},
		struct {
			nativeJSONEmbedA
			nativeJSONEmbedTagged
		}{nativeJSONEmbedA{bad}, nativeJSONEmbedTagged{"safe"}},
	}
	for _, value := range accepted {
		if err := ValidateNativeJSONStrings(value); err != nil {
			t.Fatalf("allowed native representation %T: %v", value, err)
		}
	}
	if calls != 0 {
		t.Fatal("native preflight invoked JSON marshaler")
	}
	quoted := struct {
		Count int64 `json:"count,string"`
	}{9007199254740993}
	encoded, err := Freeze(quoted, func(raw []byte) error {
		if err := ValidateJSONUTF8(raw); err != nil {
			return err
		}
		return ValidateJavaScriptJSONNumbers(raw)
	})
	if err != nil || string(encoded) != `{"count":"9007199254740993"}` {
		t.Fatalf("quoted numeric field changed: %s %v", encoded, err)
	}
	if _, err := Freeze(both, ValidateDSHJSONSnapshot); err != nil || calls != 1 {
		t.Fatalf("JSON precedence/freeze calls=%d err=%v", calls, err)
	}
	for _, value := range []any{
		bad, map[string]any{bad: true}, map[string]any{"nested": []any{bad}},
		map[nativeJSONTextString]int{nativeJSONTextString(bad): 1},
		nativeJSONTextOnly{}, map[nativeJSONTextInteger]int{1: 1}, []nativeJSONTextByte{1},
		struct{ nativeJSONEmbedA }{nativeJSONEmbedA{bad}},
		struct {
			Value nativeJSONCustomZero `json:"value,omitzero"`
		}{nativeJSONCustomZero{bad}},
	} {
		if err := ValidateNativeJSONStrings(value); !errors.Is(err, ErrUnrepresentableJSONString) {
			t.Fatalf("lossy/unsupported native %T accepted: %v", value, err)
		}
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if err := ValidateNativeJSONStrings(cyclic); err != nil {
		t.Fatalf("cycle must remain Marshal's existing contract: %v", err)
	}
}

func TestDSHSnapshotRejectsRawInvalidUTF8BeforeTokenDecoding(t *testing.T) {
	for _, raw := range [][]byte{{'"', 0xff, '"'}, append([]byte(`{"nested":"`), 0xff, '"', '}')} {
		if !json.Valid(raw) {
			t.Fatal("fixture must reach encoding/json's UTF8 replacement edge")
		}
		if err := ValidateDSHJSONSnapshot(raw); !errors.Is(err, ErrUnrepresentableJSONString) {
			t.Fatalf("invalid raw UTF8 accepted: %v", err)
		}
	}
	for _, raw := range []string{`{"opaque":"\ud800"}`, `{"opaque":{"\ud800":"literal"}}`, `{"replacement":"\ufffd"}`} {
		if err := ValidateDSHJSONSnapshot([]byte(raw)); err != nil {
			t.Fatalf("valid encoded opaque Unicode rejected: %v", err)
		}
	}
}

type nativeJSONRawEmitter struct {
	Calls *int
	Raw   []byte
}

func (v nativeJSONRawEmitter) MarshalJSON() ([]byte, error) { *v.Calls++; return v.Raw, nil }
func TestNativeJSONStringsMarshalerOutputIsCheckedExactlyOnce(t *testing.T) {
	calls := 0
	value := nativeJSONRawEmitter{Calls: &calls, Raw: []byte{'"', 0xff, '"'}}
	if err := ValidateNativeJSONStrings(value); err != nil || calls != 0 {
		t.Fatalf("preflight invoked/inspected marshaler: %v calls=%d", err, calls)
	}
	if _, err := Freeze(value, ValidateDSHJSONSnapshot); !errors.Is(err, ErrUnrepresentableJSONString) || calls != 1 {
		t.Fatalf("actual sole marshal output not checked: %v calls=%d", err, calls)
	}
}

func TestNativeJSONStringsNilEncoderAndOmitZeroBoundaries(t *testing.T) {
	for _, value := range []any{(*nativeJSONBothEncoders)(nil), (*nativeJSONTextOnly)(nil)} {
		if err := ValidateNativeJSONStrings(value); err != nil {
			t.Fatal(err)
		}
		data, err := Freeze(value, ValidateDSHJSONSnapshot)
		if err != nil || string(data) != "null" {
			t.Fatalf("nil encoder changed its JSON null contract: %s %v", data, err)
		}
	}
	for _, value := range []any{
		struct {
			Value *nativeJSONCustomZero `json:"value,omitzero"`
		}{},
		struct {
			Value interface{ IsZero() bool } `json:"value,omitzero"`
		}{},
		struct {
			Value interface{ IsZero() bool } `json:"value,omitzero"`
		}{Value: (*nativeJSONCustomZero)(nil)},
	} {
		if err := ValidateNativeJSONStrings(value); err != nil {
			t.Fatalf("nil omission invoked/rejected custom IsZero: %v", err)
		}
		data, err := Freeze(value, ValidateDSHJSONSnapshot)
		if err != nil || string(data) != "{}" {
			t.Fatalf("nil omitzero contract changed: %s %v", data, err)
		}
	}
}

type nativeJSONPointerZero struct{ Shared *string }

func (v *nativeJSONPointerZero) IsZero() bool { *v.Shared = string([]byte{0xff}); return false }
func TestNativeJSONStringsRejectsBoxedCustomZeroWithoutMutatingSource(t *testing.T) {
	shared := "safe"
	value := struct {
		Value nativeJSONPointerZero `json:"value,omitzero"`
	}{nativeJSONPointerZero{Shared: &shared}}
	if err := ValidateNativeJSONStrings(value); !errors.Is(err, ErrUnrepresentableJSONString) {
		t.Fatalf("unaddressable pointer-receiver IsZero was admitted: %v", err)
	}
	if shared != "safe" {
		t.Fatal("preflight invoked boxed custom IsZero")
	}
}

func TestNativeJSONStringsLiteralDashTagAndStaticEncoderPrecedence(t *testing.T) {
	bad := string([]byte{0xff})
	visible := struct {
		Text string `json:"-,omitempty"`
	}{bad}
	if err := ValidateNativeJSONStrings(visible); !errors.Is(err, ErrUnrepresentableJSONString) {
		t.Fatalf("visible literal dash tag escaped: %v", err)
	}
	ignored := struct {
		Text string `json:"-"`
	}{bad}
	if err := ValidateNativeJSONStrings(ignored); err != nil {
		t.Fatalf("ignored exact dash tag rejected: %v", err)
	}
	calls := 0
	both := nativeJSONBothEncoders{Calls: &calls, Hidden: bad}
	staticText := struct {
		Value encoding.TextMarshaler `json:"value"`
	}{both}
	if err := ValidateNativeJSONStrings(staticText); !errors.Is(err, ErrUnrepresentableJSONString) || calls != 0 {
		t.Fatalf("static Text encoder was bypassed or called: %v calls=%d", err, calls)
	}
	staticTextNil := struct {
		Value encoding.TextMarshaler `json:"value"`
	}{(*nativeJSONTextOnly)(nil)}
	if err := ValidateNativeJSONStrings(staticTextNil); !errors.Is(err, ErrUnrepresentableJSONString) {
		t.Fatalf("static Text interface dynamic nil escaped its actual encoder: %v", err)
	}
	staticJSON := struct {
		Value json.Marshaler `json:"value"`
	}{both}
	if err := ValidateNativeJSONStrings(staticJSON); err != nil || calls != 0 {
		t.Fatalf("static JSON encoder was pre-called: %v calls=%d", err, calls)
	}
	data, err := Freeze(staticJSON, ValidateDSHJSONSnapshot)
	if err != nil || calls != 1 || string(data) != `{"value":{"safe":true}}` {
		t.Fatalf("static JSON priority changed: %s %v calls=%d", data, err, calls)
	}
}

