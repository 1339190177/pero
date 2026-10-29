package sessionview

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/phodal/pero/harness/go/session"
)

func event(seq uint64, typ, data string) session.Event {
	return session.Event{Seq: seq, Type: typ, Time: time.Date(2026, 10, 28, 1, 2, 3, 0, time.UTC), Data: json.RawMessage(data), SurfaceOp: json.RawMessage(`"append"`)}
}
func appendEvents(t *testing.T, h *History, events ...session.Event) {
	t.Helper()
	if err := h.Append(events); err != nil {
		t.Fatal(err)
	}
}
func page(t *testing.T, h *History, r Request) Page {
	t.Helper()
	p, err := h.Window(r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The count, source-group and turn-boundary cases follow official DSH
// packages/api/session-controller/src/history.ts paginate at f3803c9.
// The UTF-16 byte budget is a retained Point extension, not current DSH parity.
func TestHistoryPagination(t *testing.T) {
	h := NewHistory(Input{ID: "history"})
	for i := uint64(1); i <= 12; i++ {
		typ, data := "user/message", `{"content":[{"type":"text","text":"😀"}]}`
		if i == 1 || i == 7 {
			typ, data = "turn/start", `{"turn":1}`
		}
		appendEvents(t, h, event(i, typ, data))
	}
	before, through := int64(7), int64(8)
	for _, tc := range []struct {
		name    string
		request Request
		first   int64
		count   int
		more    bool
	}{
		{"all", Request{}, 0, 12, false},
		{"message count", Request{MaxMessages: 2}, 10, 2, true},
		{"exclusive before", Request{BeforeSeq: &before, MaxMessages: 2}, 4, 3, true},
		{"inclusive through", Request{ThroughSeq: &through, MaxMessages: 2}, 7, 2, true},
		{"turn boundary", Request{MaxMessages: 10, MinMessages: 2, MinTurns: 1}, 6, 6, true},
		{"byte minimum", Request{MaxBytes: 1}, 5, 7, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := page(t, h, tc.request)
			if p.FirstSeq != tc.first || len(p.Records) != tc.count || p.HasMore != tc.more {
				t.Fatalf("page=%+v records=%d", p, len(p.Records))
			}
		})
	}
	// A later append-origin message owns earlier source events; keep the group.
	grouped := event(13, "user/message", `{"content":[]}`)
	grouped.SourceEventSeqs = []uint64{9, 10}
	appendEvents(t, h, grouped)
	p := page(t, h, Request{MaxMessages: 1})
	if p.FirstSeq != 8 || len(p.Records) != 5 {
		t.Fatalf("split source group: %+v", p)
	}
}

func TestHistoryAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []session.Event
		want   error
	}{
		{"gap", []session.Event{event(2, "user/message", `{}`)}, ErrDiverged},
		{"unsafe integer", []session.Event{event(1, "user/message", `{"n":9007199254740993}`)}, ErrIncompatibleJSON},
		{"negative zero", []session.Event{event(1, "user/message", `{"n":-0}`)}, ErrIncompatibleJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHistory(Input{})
			if err := h.Append(tc.events); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if h.LastNativeSeq() != 0 || h.Len() != 0 {
				t.Fatal("invalid record advanced history")
			}
		})
	}
	h := NewHistory(Input{})
	e := event(1, "user/message", `{"content":[]}`)
	appendEvents(t, h, e)
	appendEvents(t, h, e)
	if h.Len() != 1 {
		t.Fatal("overlapping append duplicated history")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.AppendContext(ctx, []session.Event{event(2, "user/message", `{}`)}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	minus, beyond := int64(-2), int64(1)
	for _, r := range []Request{{ThroughSeq: &minus}, {ThroughSeq: &beyond}, {BeforeSeq: &minus}, {MaxMessages: -1}, {MaxBytes: -1}, {MinTurns: 1}, {MinMessages: 2, MinTurns: 1, MaxMessages: 1}} {
		if _, err := h.Window(r); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("request=%+v err=%v", r, err)
		}
	}
}

func TestHistoryDigest(t *testing.T) {
	h := NewHistory(Input{})
	digest := strings.Repeat("a", 64)
	appendEvents(t, h,
		event(1, "approval/asked", `{"id":"resolved"}`), event(2, "approval/asked", `{"id":"pending"}`), event(3, "approval/decided", `{"id":"resolved"}`),
		event(4, "session/model-selected", `{"model":"old"}`), event(5, "session/model-selected", `{"model":"new"}`),
		event(6, "agent-preset/selected", `{"id":"preset"}`), event(7, "user/message", `{"content":[{"type":"text","text":"  latest  "}]}`),
		event(8, "tool/result", `{"toolCallId":"canvas","content":[],"meta":{"kind":"homology.canvas.render.v1","artifactDigest":"`+digest+`","revision":"`+digest+`","title":"  Figure  "}}`),
		event(9, "tool/result", `{"toolCallId":"failed","content":[],"isError":true,"meta":{"kind":"homology.canvas.render.v1","artifactDigest":"`+digest+`","revision":"`+digest+`"}}`))
	d := h.Digest()
	if len(d.Approvals) != 1 || d.Approvals[0].NativeSeq != 2 || len(h.PendingApprovals(3)) != 0 {
		t.Fatalf("approvals=%+v", d.Approvals)
	}
	if string(d.ModelSelected) != `{"model":"new"}` || string(d.AgentPreset) != `{"id":"preset"}` || d.LastUser.Preview != "latest" || d.LastUser.Seq != 7 {
		t.Fatalf("digest=%+v", d)
	}
	if len(d.Canvases) != 1 || d.Canvases[0].Sequence != 8 || d.Canvases[0].Title != "Figure" {
		t.Fatalf("canvases=%+v", d.Canvases)
	}
	if !reflect.DeepEqual(d, h.Digest()) {
		t.Fatal("digest not stable")
	}
}
