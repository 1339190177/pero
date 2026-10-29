package sessionview

import (
	"github.com/phodal/pero/harness/go/session"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiskHistoryContinuation(t *testing.T) {
	h := NewHistory(Input{ID: "disk", Provider: "provider", Model: "model"})
	appendEvents(t, h, event(1, "step/start", `{"turn":1,"step":1}`), event(2, "assistant/chunk", `{"chunk":{"content":[{"type":"text","text":"hello"}]}}`))
	path := filepath.Join(t.TempDir(), "history")
	if err := h.Save(path, []byte(`{"owner":"host"}`)); err != nil {
		t.Fatal(err)
	}
	old, extra, err := OpenDisk(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(extra) != `{"owner":"host"}` || string(old.StreamSnapshot()) != string(h.StreamSnapshot()) {
		t.Fatal("lost snapshot metadata")
	}
	final := event(3, "assistant/message", `{"message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`)
	appendEvents(t, h, final)
	if err := h.Save(path, nil); err != nil {
		t.Fatal(err)
	}
	// A previously opened snapshot owns its immutable checkpoint, even after a
	// new index is published. Both continuations must produce identical events.
	appendEvents(t, old, final)
	reopened, _, err := OpenDisk(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page(t, old, Request{}), page(t, reopened, Request{})) {
		t.Fatal("checkpoint continuation differs from uninterrupted history")
	}
	if !reflect.DeepEqual(old.Digest(), reopened.Digest()) {
		t.Fatal("digest lost in cache")
	}
	if n, ok := reopened.NativeSeq(reopened.Cursor()); !ok || n != 3 {
		t.Fatalf("native mapping=%d %v", n, ok)
	}
}

func TestDiskCorruption(t *testing.T) {
	for _, kind := range []string{"index", "blob", "checkpoint"} {
		t.Run(kind, func(t *testing.T) {
			h := NewHistory(Input{})
			appendEvents(t, h, event(1, "user/message", `{"content":[]}`))
			path := filepath.Join(t.TempDir(), "history")
			if err := h.Save(path, nil); err != nil {
				t.Fatal(err)
			}
			target := path
			if kind == "blob" {
				target = filepath.Join(filepath.Dir(path), h.segments[0].Name)
			}
			if kind == "checkpoint" {
				target = filepath.Join(filepath.Dir(path), h.checkpoint)
			}
			raw, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			raw[len(raw)-1] ^= 1
			if err := os.WriteFile(target, raw, 0600); err != nil {
				t.Fatal(err)
			}
			loaded, _, err := OpenDisk(path)
			if kind == "index" {
				if err == nil {
					t.Fatal("corrupt index accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "blob" {
				if _, err = loaded.Window(Request{}); err == nil {
					t.Fatal("corrupt event accepted")
				}
			} else {
				if err = loaded.Append([]session.Event{event(2, "user/message", `{"content":[]}`)}); err == nil {
					t.Fatal("corrupt checkpoint accepted")
				}
			}
		})
	}
}
