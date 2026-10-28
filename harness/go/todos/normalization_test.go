package todos

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/phodal/pero/harness/go/harness"
)

func TestTodoNormalizationExactUnicodeAndDomainMessages(t *testing.T) {
	for _, tc := range []struct {
		name, input, want, message string
	}{
		{"ASCII blank", " \t\r\n", "", "invalid todo: `content` must be a non-empty string"},
		{"BOM blank", "\ufeff", "", "invalid todo: `content` must be a non-empty string"},
		{"mixed blank", " \t\ufeff\u00a0\u2028\u2029\u3000", "", "invalid todo: `content` must be a non-empty string"},
		{"NEL", "\u0085", "\u0085", ""},
		{"mixed NEL", " \t\u0085\ufeff ", "\u0085", ""},
		{"BOM wrapper", "\ufeff keep \ufeff", "keep", ""},
		{"NEL wrapper", "\u0085keep\u0085", "\u0085keep\u0085", ""},
		{"inner BOM", "a\ufeffb", "a\ufeffb", ""},
		{"zero width", "\u200b", "\u200b", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []Item{{Content: tc.input, Status: Pending}}
			got, counts, err := normalizeWithMode(source, false, ContentNormalizationDSHAlpha3)
			if tc.message != "" {
				if domain, ok := err.(*todoDomainError); !ok || domain.Error() != tc.message || got != nil || counts != nil {
					t.Fatalf("got=%+v counts=%v error=%v", got, counts, err)
				}
			} else if err != nil || !reflect.DeepEqual(got, []Item{{Content: tc.want, Status: Pending}}) || counts[Pending] != 1 {
				t.Fatalf("got=%+v counts=%v error=%v", got, counts, err)
			}
			legacy, _, legacyErr := normalize(source, false)
			wantLegacy := strings.TrimSpace(tc.input)
			if wantLegacy == "" {
				if legacyErr == nil || legacyErr.Error() != "invalid todo: content must be a non-empty string" {
					t.Fatalf("legacy changed: %+v %v", legacy, legacyErr)
				}
			} else if legacyErr != nil || legacy[0].Content != wantLegacy {
				t.Fatalf("legacy changed: %+v %v", legacy, legacyErr)
			}
			if source[0].Content != tc.input {
				t.Fatal("normalizer mutated caller input")
			}
		})
	}
	const text = "x\u0085\n\t\"\\\u2028\u2029<>&\x00y"
	const quoted = "\"x\u0085\\n\\t\\\"\\\\\u2028\u2029<>&\\u0000y\""
	if got := quoteECMAScriptString(text); got != quoted {
		t.Fatalf("JSON.stringify quoting=%q want=%q", got, quoted)
	}
	for _, tc := range []struct {
		name     string
		list     []Item
		parallel bool
		message  string
	}{
		{"duplicate BOM", []Item{{"\ufeffx\ufeff", Pending}, {"x", Completed}}, false, `invalid todos: duplicate content "x"`},
		{"duplicate control", []Item{{text, Pending}, {text, Completed}}, false, "invalid todos: duplicate content " + quoted},
		{"parallel cap", []Item{{"a", InProgress}, {"b", InProgress}}, false, "invalid todos: at most one task may be in_progress (got 2)"},
		{"blank before cap", []Item{{"a", InProgress}, {"b", InProgress}, {"\ufeff", Pending}}, false, "invalid todo: `content` must be a non-empty string"},
		{"duplicate before cap", []Item{{"a", InProgress}, {"b", InProgress}, {"a", Pending}}, false, `invalid todos: duplicate content "a"`},
		{"parallel allowed", []Item{{"a", InProgress}, {"b", InProgress}}, true, ""},
		{"distinct NEL", []Item{{"\u0085x", Pending}, {"x", Pending}}, false, ""},
		{"no NFC", []Item{{"é", Pending}, {"e\u0301", Pending}}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := normalizeWithMode(tc.list, tc.parallel, ContentNormalizationDSHAlpha3)
			if tc.message == "" {
				if err != nil || !reflect.DeepEqual(got, tc.list) {
					t.Fatalf("got=%+v err=%v", got, err)
				}
			} else if err == nil || err.Error() != tc.message {
				t.Fatalf("error=%v want=%s", err, tc.message)
			}
		})
	}
}

func TestTodoNormalizationWhitespaceSetAndQuoteAllControls(t *testing.T) {
	const whitespace = "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"
	// Exact closed set, including explicit NEL exclusion. Node independently
	// compares the actual Go predicate cases with String.trim for all scalars.
	for r := rune(0); r <= 0x10ffff; r++ {
		if isECMAScriptWhitespace(r) != strings.ContainsRune(whitespace, r) {
			t.Fatalf("whitespace U+%04X", r)
		}
	}
	for r := rune(0); r < 32; r++ {
		quoted := quoteECMAScriptString(string(r))
		var decoded string
		if json.Unmarshal([]byte(quoted), &decoded) != nil || decoded != string(r) {
			t.Fatalf("control %d: %q", r, quoted)
		}
	}
}

func TestTodoLegacyRecordBytesAndDecoderStayUnchanged(t *testing.T) {
	for _, list := range [][]Item{nil, {}, {{"\ufeff<>&\u0085", Pending}}} {
		got, err := json.Marshal(writeEvent{Todos: list})
		want, oldErr := json.Marshal(struct {
			Todos []Item `json:"todos"`
		}{list})
		if err != nil || oldErr != nil || string(got) != string(want) {
			t.Fatalf("legacy bytes=%s want=%s", got, want)
		}
	}
	for _, raw := range []string{`null`, `{}`, `{"todos":null}`, `{"TODOS":[]}`, `{"todos":[],"NORMALIZATION":[]}`, `[]`, `false`, `{"todos":1}`, `{"todos":[{"content":null,"status":"pending"}]}`, `{"todos":[{"content":"\ud800","status":"pending"}]}`} {
		var old writeEvent
		oldErr := json.Unmarshal([]byte(raw), &old)
		got, mode, err := decodeWriteEvent([]byte(raw))
		if mode != ContentNormalizationLegacy || !reflect.DeepEqual(got, old) || fmt.Sprint(err) != fmt.Sprint(oldErr) {
			t.Fatalf("legacy %s: %+v/%v != %+v/%v", raw, got, err, old, oldErr)
		}
	}
}

func TestTodoTaggedReplayRejectsMalformedFormat(t *testing.T) {
	for _, raw := range []string{
		`{"normalization":null,"todos":[]}`, `{"normalization":1,"todos":[]}`, `{"normalization":true,"todos":[]}`, `{"normalization":[],"todos":[]}`, `{"normalization":{},"todos":[]}`, `{"normalization":"unknown","todos":[]}`, `{"normalization":"","todos":[]}`,
		`{"normalization":"ecmascript-trim-v1"}`, `{"normalization":"ecmascript-trim-v1","TODOS":[]}`, `{"normalization":"ecmascript-trim-v1","todos":null}`, `{"normalization":"ecmascript-trim-v1","todos":{}}`, `{"normalization":"ecmascript-trim-v1","todos":"x"}`, `{"normalization":"ecmascript-trim-v1","todos":0}`, `{"normalization":"ecmascript-trim-v1","todos":false}`,
		`{"normalization":"ecmascript-trim-v1","todos":[null]}`, `{"normalization":"ecmascript-trim-v1","todos":[[]]}`, `{"normalization":"ecmascript-trim-v1","todos":[false]}`, `{"normalization":"ecmascript-trim-v1","todos":[1]}`, `{"normalization":"ecmascript-trim-v1","todos":["x"]}`, `{"normalization":"ecmascript-trim-v1","todos":[{}]}`,
		`{"normalization":"ecmascript-trim-v1","todos":[{"content":null,"status":"pending"}]}`, `{"normalization":"ecmascript-trim-v1","todos":[{"content":"x","status":null}]}`, `{"normalization":"ecmascript-trim-v1","todos":[{"content":"x","status":"doing"}]}`,
		`{"normalization":"ecmascript-trim-v1","todos":[{"CONTENT":"x","status":"pending"}]}`, `{"normalization":"ecmascript-trim-v1","todos":[{"content":"x","status":"pending","extra":true}]}`, `{"normalization":"ecmascript-trim-v1","todos":[{"content":"\ud800","status":"pending"}]}`,
	} {
		session := harness.NewSession("malformed-format", "")
		if _, err := session.Append("todo/write", json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
		if got, found, err := Current(session); err == nil || got != nil || found || err.Error() != "todos: decode todo/write at seq 1: invalid normalized todo/write format" {
			t.Fatalf("raw=%s got=%+v/%v err=%v", raw, got, found, err)
		}
	}
	// Decoding itself also refuses invalid UTF-8 before lossy string conversion.
	raw := append([]byte(`{"normalization":"ecmascript-trim-v1","todos":[{"content":"`), 0xff)
	raw = append(raw, []byte(`","status":"pending"}]}`)...)
	if _, _, err := decodeWriteEvent(raw); err == nil {
		t.Fatal("invalid UTF-8 normalized")
	}
	for _, content := range []string{"\ufeff", "x"} {
		list := []Item{{content, Pending}}
		if content == "x" {
			list = append(list, list[0])
		}
		session := harness.NewSession("invalid-domain", "")
		if _, err := session.Append("todo/write", normalizedWriteEvent{list, ecmascriptTrimV1}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Current(session); err == nil || !strings.HasPrefix(err.Error(), "todos: invalid todo/write at seq 1:") {
			t.Fatalf("domain replay err=%v", err)
		}
	}
}

func TestTodoMixedReplayPersistsFormatAndDetachedEmpty(t *testing.T) {
	session := harness.NewSession("todo-mixed", t.TempDir())
	if got, found, err := Current(session); err != nil || found || got != nil {
		t.Fatalf("initial=%+v/%v %v", got, found, err)
	}
	root := t.TempDir()
	persistence, err := harness.NewJSONLSessionPersistence(root)
	if err != nil {
		t.Fatal(err)
	}
	detach, err := persistence.Attach(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := detach(); err != nil {
			t.Errorf("Todo JSONL detach: %v", err)
		}
	})
	for i, tc := range []struct {
		payload  any
		want     []Item
		emptyNil bool
	}{
		{writeEvent{[]Item{{"\u0085legacy\u0085", Pending}}}, []Item{{"legacy", Pending}}, false},
		{normalizedWriteEvent{[]Item{{"\u0085", InProgress}, {"active", InProgress}}, ecmascriptTrimV1}, []Item{{"\u0085", InProgress}, {"active", InProgress}}, false},
		{writeEvent{[]Item{{"\ufeffold\ufeff", Completed}}}, []Item{{"\ufeffold\ufeff", Completed}}, false},
		{normalizedWriteEvent{[]Item{}, ecmascriptTrimV1}, []Item{}, false},
		{writeEvent{[]Item{}}, nil, true},
		{normalizedWriteEvent{[]Item{{"\u0085", Pending}}, ecmascriptTrimV1}, []Item{{"\u0085", Pending}}, false},
	} {
		if _, err := session.Append("todo/write", tc.payload); err != nil {
			t.Fatal(err)
		}
		got, found, err := Current(session)
		if err != nil || !found || !reflect.DeepEqual(got, tc.want) || (got == nil) != tc.emptyNil {
			t.Fatalf("row %d got=%+v/%v err=%v", i, got, found, err)
		}
		if len(got) > 0 {
			got[0].Content = "caller mutation"
		}
		// A newly constructed persistence instance restores actual stored JSONL,
		// not a process-local normalization map or mounted Todo option.
		reader, err := harness.NewJSONLSessionPersistence(root)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := reader.Load(t.Context(), session.Header().ID)
		if err != nil {
			t.Fatal(err)
		}
		if restored == session || !reflect.DeepEqual(restored.Events(), session.Events()) {
			t.Fatal("not detached exact JSONL restoration")
		}
		current, present, err := Current(restored)
		if err != nil || !present || !reflect.DeepEqual(current, tc.want) {
			t.Fatalf("restored row %d=%+v/%v %v", i, current, present, err)
		}
		seq := session.Events()[len(session.Events())-1].Seq
		fork, err := session.ForkPublic(harness.SessionID(fmt.Sprintf("todo-fork-%d", i)), &seq)
		if err != nil {
			t.Fatal(err)
		}
		current, present, err = Current(fork)
		if err != nil || !present || !reflect.DeepEqual(current, tc.want) {
			t.Fatalf("fork row %d=%+v/%v %v", i, current, present, err)
		}
	}
	if _, err := session.Append("turn/start", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	if got, found, err := Current(session); err != nil || found || got != nil {
		t.Fatalf("reset=%+v/%v %v", got, found, err)
	}
	if _, err := session.Append("todo/write", normalizedWriteEvent{[]Item{}, ecmascriptTrimV1}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append("turn/end", map[string]any{"turn": 1, "reason": "stop"}); err != nil {
		t.Fatal(err)
	}
	if got, found, err := Current(session); err != nil || !found || got == nil || len(got) != 0 {
		t.Fatalf("turn/end=%+v/%v %v", got, found, err)
	}
}

func TestTodoDowngradeReinterpretsTaggedRowsAsLegacy(t *testing.T) {
	for _, tc := range []struct {
		content  string
		changed  bool
		rejected bool
	}{{"\u0085", false, true}, {"\u0085keep\u0085", true, false}, {"keep", false, false}} {
		raw, _ := json.Marshal(normalizedWriteEvent{[]Item{{tc.content, Pending}}, ecmascriptTrimV1})
		// This is the exact pre-change decoder + normalizer, not an old binary
		// execution claim. It documents why downgrade is not semantic-safe.
		var old writeEvent
		if err := json.Unmarshal(raw, &old); err != nil {
			t.Fatal(err)
		}
		oldList, _, oldErr := normalize(old.Todos, true)
		if (oldErr != nil) != tc.rejected {
			t.Fatalf("old reader=%+v %v", oldList, oldErr)
		}
		if !tc.rejected && (oldList[0].Content != tc.content) != tc.changed {
			t.Fatalf("old reader silently changed %q to %+v", tc.content, oldList)
		}
		payload, mode, err := decodeWriteEvent(raw)
		if err != nil {
			t.Fatal(err)
		}
		current, _, err := normalizeWithMode(payload.Todos, true, mode)
		if err != nil || current[0].Content != tc.content {
			t.Fatalf("new reader=%+v %v", current, err)
		}
	}
}

func TestTodoModeMountValidationAndJSONCannotSelect(t *testing.T) {
	h, _ := todoDomainHarness(t)
	for _, mode := range []ContentNormalizationMode{2, 255} {
		if _, err := Mount(t.Context(), h, Options{ContentNormalization: mode}); err == nil || err.Error() != "todos: unsupported content normalization mode" {
			t.Fatalf("mode=%d error=%v", mode, err)
		}
		if _, exists := h.Tools().Resolve(h.Runtime().Root(), ToolName); exists {
			t.Fatal("invalid mode registered tool")
		}
	}
	var options Options
	if err := json.Unmarshal([]byte(`{"ContentNormalization":1,"contentNormalization":1,"normalization":"ecmascript-trim-v1"}`), &options); err != nil {
		t.Fatal(err)
	}
	if options.ContentNormalization != ContentNormalizationLegacy {
		t.Fatal("JSON selected trusted mode")
	}
}

