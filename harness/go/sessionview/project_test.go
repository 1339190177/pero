package sessionview

import (
	"bytes"
	"testing"
)

func TestProjectionStreamCheckpoint(t *testing.T) {
	p := NewProjector(Input{ID: "stream", Provider: "provider", Model: "model"})
	for _, e := range []struct {
		seq       uint64
		typ, data string
	}{
		{1, "step/start", `{"turn":2,"step":3}`},
		{2, "assistant/chunk", `{"chunk":{"content":[{"type":"text","text":"hel"}]}}`},
	} {
		if _, err := p.Project(event(e.seq, e.typ, e.data)); err != nil {
			t.Fatal(err)
		}
	}
	delta, id, offset := p.StreamDelta("", 0)
	if id != "point-stream:stream:2:3" || offset != 2 || !bytes.Contains(delta, []byte(`"text":"hel"`)) {
		t.Fatalf("delta=%s id=%s offset=%d", delta, id, offset)
	}
	checkpoint, err := p.checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ProjectorFromCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.StreamSnapshot(), restored.StreamSnapshot()) {
		t.Fatal("checkpoint lost partial stream")
	}
	if _, err = restored.Project(event(3, "assistant/chunk", `{"chunk":{"content":[{"type":"text","text":"lo"}]}}`)); err != nil {
		t.Fatal(err)
	}
	delta, _, next := restored.StreamDelta(id, offset)
	if next != 3 || !bytes.Contains(delta, []byte(`"offset":2`)) || bytes.Contains(delta, []byte(`"text":"hel"`)) || !bytes.Contains(delta, []byte(`"text":"lo"`)) {
		t.Fatalf("incremental delta=%s next=%d", delta, next)
	}
	reset, _, _ := restored.StreamDelta("stale-stream", offset)
	if !bytes.Contains(reset, []byte(`"offset":0`)) {
		t.Fatalf("stale cursor did not reset: %s", reset)
	}
	out, err := restored.Project(event(4, "assistant/message", `{"message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Get("type") != "assistant/message" || out[0].Get("surfaceOp") != "append" {
		t.Fatalf("final=%v", out)
	}
	data := asObj(out[0].Get("data"))
	msg := asObj(data.Get("message"))
	source := asObj(msg.Get("source"))
	if source.Get("provider") != "provider" || source.Get("model") != "model" {
		t.Fatalf("source=%s", AppendJSON(nil, source))
	}
	records := data.Get("stream").([]any)
	if len(records) != 5 {
		t.Fatalf("final duplicates streamed text: %s", AppendJSON(nil, data))
	}
	for i, want := range []string{"block-start", "text-delta", "text-delta", "block-end", "finish"} {
		if got := asObj(asObj(records[i]).Get("chunk")).Get("type"); got != want {
			t.Fatalf("stream record %d = %v, want %s", i, got, want)
		}
	}
	if n, ok := restored.NativeSeq(1); !ok || n != 4 {
		t.Fatalf("native=%d %v", n, ok)
	}
}

func TestProjectionReplacement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		call      string
		wantError bool
	}{
		{"content only", "call", false}, {"changed identity", "different", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewProjector(Input{})
			if _, err := p.Project(event(1, "tool/result", `{"toolCallId":"call","content":[{"type":"text","text":"old"}]}`)); err != nil {
				t.Fatal(err)
			}
			e := event(2, "tool/result", `{"toolCallId":"`+tc.call+`","content":[{"type":"text","text":"new"}]}`)
			e.SurfaceOp = []byte(`{"op":"replace","start":1,"end":1}`)
			out, err := p.Project(e)
			if tc.wantError {
				if err == nil {
					t.Fatal("identity change accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = CheckCurrent(out[0]); err != nil {
				t.Fatal(err)
			}
			if got := string(AppendJSON(nil, out[0].Get("surfaceOp"))); got != `{"op":"replace","startSeq":0,"endSeq":0}` {
				t.Fatalf("replacement=%s", got)
			}
			if !bytes.Contains(AppendJSON(nil, out[0].Get("data")), []byte(`"text":"new"`)) {
				t.Fatal("replacement lost content")
			}
		})
	}
}
