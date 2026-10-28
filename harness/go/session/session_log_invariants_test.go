package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

func textMessage(role harnessmodel.Role, text string) Message {
	return Message{Role: role, Content: []ContentBlock{{Type: BlockText, Text: text}}}
}

func TestSessionAppendRejectsMalformedEvents(t *testing.T) {
	live := NewSession("append-guards", t.TempDir())
	if _, err := live.Append("", nil); err == nil {
		t.Fatal("empty event type accepted")
	}
	if _, err := live.Append("fact", make(chan int)); err == nil {
		t.Fatal("unencodable payload accepted")
	}
	if _, err := live.AppendReplacement("fact", map[string]int{"n": 1}, SurfaceRange{Start: 1, End: 1}, []uint64{1}); err == nil {
		t.Fatal("log-only event was allowed to mutate the surface")
	}
	if got := len(live.Events()); got != 0 {
		t.Fatalf("rejected appends left %d events", got)
	}
}

func TestSessionAppendIfLastSeqIsACompareAndSwap(t *testing.T) {
	live := NewSession("cas", t.TempDir())
	first, err := live.AppendIfLastSeq(0, "fact", 1)
	if err != nil || first.Seq != 1 {
		t.Fatalf("first CAS event=%+v err=%v", first, err)
	}
	if _, err := live.AppendIfLastSeq(0, "fact", 2); !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("stale CAS error = %v, want ErrSessionChanged", err)
	}
	if second, err := live.AppendIfLastSeq(1, "fact", 2); err != nil || second.Seq != 2 {
		t.Fatalf("fresh CAS event=%+v err=%v", second, err)
	}
}

func TestSessionAppendSourceSeqValidation(t *testing.T) {
	live := NewSession("source-seqs", t.TempDir())
	if _, err := live.Append("fact", 1); err != nil {
		t.Fatal(err)
	}
	for name, sources := range map[string][]uint64{
		"zero":      {0},
		"self/next": {2},
		"duplicate": {1, 1},
	} {
		if _, err := live.AppendSurface("user/message", textMessage("user", name), sources); err == nil {
			t.Fatalf("%s source seq accepted", name)
		}
	}
	event, err := live.AppendSurface("user/message", textMessage("user", "ok"), []uint64{1})
	if err != nil || !reflect.DeepEqual(event.SourceEventSeqs, []uint64{1}) {
		t.Fatalf("valid provenance event=%+v err=%v", event, err)
	}
}

func TestSessionSurfaceReplacementRequiresCitingEveryShadowedEvent(t *testing.T) {
	live := NewSession("replace", t.TempDir())
	for _, text := range []string{"a", "b", "c"} {
		if _, err := live.Append("user/message", textMessage("user", text)); err != nil {
			t.Fatal(err)
		}
	}
	if got := live.SurfaceSeqs(); !reflect.DeepEqual(got, []uint64{1, 2, 3}) {
		t.Fatalf("surface=%v", got)
	}
	if _, err := live.AppendReplacement("user/message", textMessage("user", "s"), SurfaceRange{Start: 1, End: 2}, []uint64{1}); err == nil {
		t.Fatal("replacement that skipped a shadowed seq was accepted")
	}
	if _, err := live.AppendReplacement("user/message", textMessage("user", "s"), SurfaceRange{Start: 2, End: 1}, []uint64{1, 2}); err == nil {
		t.Fatal("reversed range accepted")
	}
	if _, err := live.AppendReplacement("user/message", textMessage("user", "s"), SurfaceRange{Start: 8, End: 9}, []uint64{1}); err == nil {
		t.Fatal("missing range accepted")
	}
	replacement, err := live.AppendReplacement("user/message", textMessage("user", "ab"), SurfaceRange{Start: 1, End: 2}, []uint64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := live.SurfaceSeqs(); !reflect.DeepEqual(got, []uint64{replacement.Seq, 3}) {
		t.Fatalf("surface after replace=%v, want [%d 3]", got, replacement.Seq)
	}
	if len(live.Events()) != 4 {
		t.Fatal("replacement must retain every shadowed raw event")
	}
	messages, err := live.DeriveMessages()
	if err != nil || len(messages) != 2 || messages[0].Content[0].Text != "ab" || messages[1].Content[0].Text != "c" {
		t.Fatalf("derived=%+v err=%v", messages, err)
	}
}

func TestSessionAppendPrunedToolResultCommitsPriceFactThenReplacement(t *testing.T) {
	live := NewSession("prune", t.TempDir())
	if _, err := live.AppendPrunedToolResult(1, json.RawMessage(`{}`), 5); err == nil {
		t.Fatal("prune outside an open turn accepted")
	}
	mustAppend(t, live, "turn/start", map[string]int{"turn": 1})
	call := mustAppend(t, live, "user/message", textMessage("user", "not a tool result"))
	result := mustAppend(t, live, "tool/result", ContentBlock{Type: BlockToolResult, ToolCallID: "c1", Content: []ContentBlock{{Type: BlockText, Text: "big output"}}})

	for name, run := range map[string]func() error{
		"zero seq":        func() error { _, err := live.AppendPrunedToolResult(0, json.RawMessage(`{}`), 5); return err },
		"zero tokens":     func() error { _, err := live.AppendPrunedToolResult(result.Seq, json.RawMessage(`{}`), 0); return err },
		"invalid json":    func() error { _, err := live.AppendPrunedToolResult(result.Seq, json.RawMessage(`{`), 5); return err },
		"not tool result": func() error { _, err := live.AppendPrunedToolResult(call.Seq, json.RawMessage(`{}`), 5); return err },
		"out of range":    func() error { _, err := live.AppendPrunedToolResult(99, json.RawMessage(`{}`), 5); return err },
	} {
		if err := run(); err == nil {
			t.Fatalf("%s: prune accepted", name)
		}
	}

	var seen []string
	unsubscribe := live.Subscribe(func(event Event) { seen = append(seen, event.Type) })
	defer unsubscribe()
	replacement, err := live.AppendPrunedToolResult(result.Seq, json.RawMessage(`{"type":"tool-result","toolCallId":"c1"}`), 5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []string{"compaction/prune", "tool/result"}) {
		t.Fatalf("subscriber saw %v, want price fact before replacement", seen)
	}
	if got := live.SurfaceSeqs(); got[len(got)-1] != replacement.Seq || containsSeq(got, result.Seq) {
		t.Fatalf("surface %v still exposes pruned seq %d", got, result.Seq)
	}
	if _, err := live.AppendPrunedToolResult(result.Seq, json.RawMessage(`{}`), 5); err == nil || !strings.Contains(err.Error(), "surface") {
		t.Fatalf("re-pruning a shadowed result error = %v", err)
	}
}

func TestSessionRequestContextAndHeaderReturnLatestAndValidate(t *testing.T) {
	live := NewSession("request-ctx", t.TempDir())
	if _, found, err := live.RequestContext(); err != nil || found {
		t.Fatalf("empty RequestContext found=%t err=%v", found, err)
	}
	if _, found, err := live.RequestHeader(); err != nil || found {
		t.Fatalf("empty RequestHeader found=%t err=%v", found, err)
	}
	if _, err := live.Append("request/context", RequestContext{Provider: "p", Model: "m"}); err == nil {
		t.Fatal("request/context outside an open turn accepted")
	}
	mustAppend(t, live, "turn/start", map[string]int{"turn": 1})
	for name, bad := range map[string]RequestContext{
		"no provider":     {Model: "m"},
		"no model":        {Provider: "p"},
		"negative window": {Provider: "p", Model: "m", ContextWindow: -1},
	} {
		if _, err := live.Append("request/context", bad); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	mustAppend(t, live, "request/context", RequestContext{Provider: "p", Model: "old", ContextWindow: 10})
	mustAppend(t, live, "request/context", RequestContext{Provider: "p", Model: "new", ContextWindow: 20})
	got, found, err := live.RequestContext()
	if err != nil || !found || got.Model != "new" || got.ContextWindow != 20 {
		t.Fatalf("RequestContext=%+v found=%t err=%v", got, found, err)
	}
	mustAppend(t, live, "request/header", RequestHeaderEvent{Turn: 1, Step: 1, Model: "first"})
	mustAppend(t, live, "request/header", RequestHeaderEvent{Turn: 1, Step: 2, Model: "second", StartsSeries: true})
	header, found, err := live.RequestHeader()
	if err != nil || !found || header.Step != 2 || header.Model != "second" || !header.StartsSeries {
		t.Fatalf("RequestHeader=%+v found=%t err=%v", header, found, err)
	}
	latest, ok := live.LatestEvent("request/header")
	if !ok || latest.Seq != 5 {
		t.Fatalf("LatestEvent=%+v ok=%t", latest, ok)
	}
	if _, ok := live.LatestEvent("never"); ok {
		t.Fatal("LatestEvent invented an event")
	}
}

func TestSessionCodeDispatchEventsRequireOpenTurnAndCoherentCallTree(t *testing.T) {
	live := NewSession("dispatch", t.TempDir())
	start := func(root, parent, sub string) map[string]any {
		return map[string]any{"rootCallId": root, "parentCallId": parent, "subCallId": sub, "name": "read", "arguments": map[string]string{}}
	}
	if _, err := live.Append("tool/code-dispatch-start", start("r", "r", "s1")); err == nil {
		t.Fatal("dispatch outside an open turn accepted")
	}
	mustAppend(t, live, "turn/start", map[string]int{"turn": 1})
	if _, err := live.Append("tool/code-dispatch-start", map[string]any{"rootCallId": "r"}); err == nil {
		t.Fatal("dispatch missing required fields accepted")
	}
	if _, err := live.Append("tool/code-dispatch", start("r", "r", "s1")); err == nil {
		t.Fatal("completed dispatch without isError/content accepted")
	}
	mustAppend(t, live, "tool/code-dispatch-start", start("r", "r", "s1"))
	if _, err := live.Append("tool/code-dispatch-start", start("other", "other", "s1")); err == nil || !strings.Contains(err.Error(), "changed rootCallId") {
		t.Fatalf("root change error = %v", err)
	}
	if _, err := live.Append("tool/code-dispatch-start", start("r", "stranger", "s2")); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("foreign parent error = %v", err)
	}
	mustAppend(t, live, "tool/code-dispatch-start", start("r", "s1", "s2"))
	done := start("r", "s1", "s2")
	done["isError"] = false
	done["content"] = []ContentBlock{{Type: BlockText, Text: "ok"}}
	mustAppend(t, live, "tool/code-dispatch", done)
}

func TestSessionLastTurnTakesMaximumAndRejectsInvalidTurn(t *testing.T) {
	live := NewSession("last-turn", t.TempDir())
	if turn, err := live.LastTurn(); err != nil || turn != 0 {
		t.Fatalf("empty LastTurn=%d err=%v", turn, err)
	}
	mustAppend(t, live, "turn/start", map[string]int{"turn": 3})
	mustAppend(t, live, "note", map[string]any{"turn": nil})
	mustAppend(t, live, "note", "plain string payload")
	mustAppend(t, live, "turn/end", map[string]int{"turn": 2})
	if turn, err := live.LastTurn(); err != nil || turn != 3 {
		t.Fatalf("LastTurn=%d err=%v, want max 3", turn, err)
	}
	mustAppend(t, live, "note", map[string]int{"turn": -1})
	if _, err := live.LastTurn(); err == nil {
		t.Fatal("negative turn accepted")
	}
}

func TestSessionSubscribeDeliversInOrderSurvivesPanicsAndUnsubscribes(t *testing.T) {
	live := NewSession("subscribers", t.TempDir())
	if cancel := live.Subscribe(nil); cancel == nil {
		t.Fatal("nil listener must still return a cancel func")
	}
	var calls atomic.Int64
	var seqs []uint64
	live.Subscribe(func(Event) { calls.Add(1); panic("listener bug") })
	cancel := live.Subscribe(func(event Event) { seqs = append(seqs, event.Seq) })
	mustAppend(t, live, "fact", 1)
	mustAppend(t, live, "fact", 2)
	cancel()
	cancel() // idempotent
	mustAppend(t, live, "fact", 3)
	if calls.Load() != 3 {
		t.Fatalf("panicking subscriber calls=%d, want all 3 deliveries", calls.Load())
	}
	if !reflect.DeepEqual(seqs, []uint64{1, 2}) {
		t.Fatalf("surviving subscriber saw %v, want [1 2]", seqs)
	}
}

func TestSessionEventsReturnsDetachedCopies(t *testing.T) {
	live := NewSession("detached", t.TempDir())
	mustAppend(t, live, "fact", map[string]string{"k": "v"})
	events := live.Events()
	events[0].Data[0] = 'X'
	if got := string(live.Events()[0].Data); got != `{"k":"v"}` {
		t.Fatalf("caller mutation reached the log: %s", got)
	}
	latest, _ := live.LatestEvent("fact")
	latest.Data[0] = 'Y'
	if got := string(live.Events()[0].Data); got != `{"k":"v"}` {
		t.Fatalf("LatestEvent aliases the log: %s", got)
	}
}

func TestSessionConfigureHooksOnlyBeforeEventsOrPublication(t *testing.T) {
	fresh := NewSession("cfg", t.TempDir())
	if err := fresh.ConfigureManagedCWD(); err != nil || !fresh.Header().ManagedCWD {
		t.Fatalf("managed cwd err=%v header=%+v", err, fresh.Header())
	}
	if err := fresh.ConfigureDelegation("", 1); err == nil {
		t.Fatal("delegation without parent accepted")
	}
	if err := fresh.ConfigureDelegation("parent", 0); err != nil {
		t.Fatal(err)
	}
	header := fresh.Header()
	if header.ParentSession != "parent" || header.Origin != "subagent" || header.DelegationDepth != 1 {
		t.Fatalf("delegation header=%+v, want depth defaulted to 1", header)
	}
	mustAppend(t, fresh, "fact", 1)
	if err := fresh.ConfigureManagedCWD(); err == nil {
		t.Fatal("managed cwd changed after the first event")
	}
	if err := fresh.ConfigureDelegation("parent", 2); err == nil {
		t.Fatal("delegation changed after the first event")
	}

	store := NewSessionStore()
	published, err := store.Create("published", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := published.ConfigureManagedCWD(); err == nil {
		t.Fatal("managed cwd changed after publication")
	}
}

func TestSessionRestoreSessionRevalidatesHeaderAndLog(t *testing.T) {
	source := NewSession("restore-src", t.TempDir())
	appendTurn(t, source, 1, "hello")
	header, events := source.Header(), source.Events()

	restored, err := RestoreSession(header, events)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Restored() || restored.FirstLiveSeq() != uint64(len(events)+1) || !reflect.DeepEqual(restored.SurfaceSeqs(), source.SurfaceSeqs()) {
		t.Fatalf("restored=%t firstLive=%d surface=%v", restored.Restored(), restored.FirstLiveSeq(), restored.SurfaceSeqs())
	}
	if source.Restored() || source.FirstLiveSeq() != 1 {
		t.Fatalf("live session reports restored=%t firstLive=%d", source.Restored(), source.FirstLiveSeq())
	}
	var nilSession *Session
	if nilSession.Restored() || nilSession.FirstLiveSeq() != 1 {
		t.Fatal("nil session provenance wrong")
	}
	live := NewSession("mark", t.TempDir())
	live.MarkRestored()
	if !live.Restored() {
		t.Fatal("MarkRestored had no effect")
	}

	badHeader := header
	badHeader.ID = ""
	if _, err := RestoreSession(badHeader, events); err == nil {
		t.Fatal("empty id accepted")
	}
	badHeader = header
	badHeader.Version = SessionFormatVersion + 1
	if _, err := RestoreSession(badHeader, events); err == nil {
		t.Fatal("unknown format version accepted")
	}
	gap := append([]Event(nil), events...)
	gap[1].Seq = 9
	if _, err := RestoreSession(header, gap); err == nil || !strings.Contains(err.Error(), "non-contiguous") {
		t.Fatalf("gap error = %v", err)
	}
	badSurface := append([]Event(nil), events...)
	badSurface[1].SurfaceOp = json.RawMessage(`{"op":"replace","start":7,"end":7}`)
	if _, err := RestoreSession(header, badSurface); err == nil {
		t.Fatal("replacement of a missing surface range accepted")
	}
}

func TestSessionDurabilityCheckpointLifecycle(t *testing.T) {
	live := NewSession("durability", t.TempDir())
	var nilSession *Session
	if nilSession.DurabilityAttached() || live.DurabilityAttached() {
		t.Fatal("fresh session reports durability")
	}
	if err := live.Flush(context.Background()); err != nil {
		t.Fatalf("unattached Flush = %v", err)
	}
	if _, err := live.InstallDurabilityCheckpoint(nil); err == nil {
		t.Fatal("nil checkpoint accepted")
	}
	var flushed atomic.Int64
	detach, err := live.InstallDurabilityCheckpoint(func(context.Context) error { flushed.Add(1); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.InstallDurabilityCheckpoint(func(context.Context) error { return nil }); !errors.Is(err, ErrSessionDurabilityAttached) {
		t.Fatalf("second checkpoint error = %v", err)
	}
	if err := live.Flush(nil); err != nil || flushed.Load() != 1 || !live.DurabilityAttached() {
		t.Fatalf("flush err=%v flushed=%d attached=%t", err, flushed.Load(), live.DurabilityAttached())
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := live.Flush(cancelled); !errors.Is(err, context.Canceled) || flushed.Load() != 1 {
		t.Fatalf("cancelled flush err=%v flushed=%d", err, flushed.Load())
	}
	detach()
	detach()
	if live.DurabilityAttached() {
		t.Fatal("detach left the checkpoint installed")
	}
}

func TestDecodeSurfaceMessageHandlesEachSurfaceKindAndRejectsOthers(t *testing.T) {
	user := mustEvent(t, "user/message", textMessage("user", "hi"))
	if m, err := DecodeSurfaceMessage(user); err != nil || m.Content[0].Text != "hi" {
		t.Fatalf("user decode=%+v err=%v", m, err)
	}
	assistant := mustEvent(t, "assistant/message", map[string]any{"message": textMessage("assistant", "yo")})
	if m, err := DecodeSurfaceMessage(assistant); err != nil || m.Content[0].Text != "yo" {
		t.Fatalf("assistant decode=%+v err=%v", m, err)
	}
	tool := mustEvent(t, "tool/result", ContentBlock{Type: BlockToolResult, ToolCallID: "c"})
	if m, err := DecodeSurfaceMessage(tool); err != nil || m.Role != RoleTool || m.Content[0].ToolCallID != "c" {
		t.Fatalf("tool decode=%+v err=%v", m, err)
	}
	if _, err := DecodeSurfaceMessage(mustEvent(t, "turn/start", 1)); err == nil {
		t.Fatal("non-surface event decoded as a message")
	}
	for _, eventType := range []string{"user/message", "assistant/message", "tool/result"} {
		if _, err := DecodeSurfaceMessage(Event{Seq: 1, Type: eventType, Data: json.RawMessage(`"scalar"`)}); err == nil {
			t.Fatalf("%s accepted a scalar payload", eventType)
		}
		if !IsSurfaceEventType(eventType) {
			t.Fatalf("%s must be a surface type", eventType)
		}
	}
	if IsSurfaceEventType("turn/start") {
		t.Fatal("turn/start is not a surface type")
	}
}

func TestSessionTryLockCompactionIsExclusive(t *testing.T) {
	live := NewSession("compaction-lock", t.TempDir())
	release, ok := live.TryLockCompaction()
	if !ok {
		t.Fatal("first lock refused")
	}
	if _, second := live.TryLockCompaction(); second {
		t.Fatal("second concurrent compaction lock granted")
	}
	release()
	release() // releasing twice must not unlock someone else's hold
	again, ok := live.TryLockCompaction()
	if !ok {
		t.Fatal("lock not reusable after release")
	}
	if _, third := live.TryLockCompaction(); third {
		t.Fatal("double release freed a later holder")
	}
	again()
	var nilSession *Session
	if _, ok := nilSession.TryLockCompaction(); ok {
		t.Fatal("nil session granted a lock")
	}
}

func mustAppend(t *testing.T, s *Session, eventType string, value any) Event {
	t.Helper()
	event, err := s.Append(eventType, value)
	if err != nil {
		t.Fatalf("append %s: %v", eventType, err)
	}
	return event
}

func mustEvent(t *testing.T, eventType string, value any) Event {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return Event{Seq: 1, Type: eventType, Data: raw}
}

func containsSeq(seqs []uint64, want uint64) bool {
	for _, seq := range seqs {
		if seq == want {
			return true
		}
	}
	return false
}
