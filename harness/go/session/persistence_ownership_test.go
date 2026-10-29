package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestJSONLPersistenceExcludesOtherWritersAndRejectsStaleResume(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	owner, err := NewJSONLSessionPersistence(dir)
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewJSONLSessionPersistence(dir)
	if err != nil {
		t.Fatal(err)
	}
	original := NewSession("one-writer", dir)
	detach, err := owner.Attach(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = detach() })
	stale, err := other.Load(ctx, original.Header().ID)
	if err != nil {
		t.Fatal(err)
	}
	if closeSecond, err := other.Attach(ctx, stale); !errors.Is(err, ErrSessionAlreadyOwned) {
		if closeSecond != nil {
			_ = closeSecond()
		}
		t.Fatalf("competing writer error=%v", err)
	}
	if _, err := original.Append("turn/start", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := original.Append("turn/end", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	if err := detach(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(owner.path(original.Header().ID))
	if err != nil {
		t.Fatal(err)
	}
	if closeStale, err := other.Attach(ctx, stale); !errors.Is(err, ErrSessionPersistenceConflict) {
		if closeStale != nil {
			_ = closeStale()
		}
		t.Fatalf("stale writer error=%v", err)
	}
	after, err := os.ReadFile(owner.path(original.Header().ID))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("refused attach changed log: %v", err)
	}
	fresh, err := other.Load(ctx, original.Header().ID)
	if err != nil {
		t.Fatal(err)
	}
	closeFresh, err := other.Attach(ctx, fresh)
	if err != nil {
		t.Fatalf("failed attach retained lease: %v", err)
	}
	if _, err := fresh.Append("turn/start", map[string]int{"turn": 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Append("turn/end", map[string]int{"turn": 2}); err != nil {
		t.Fatal(err)
	}
	if err := closeFresh(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := owner.Load(ctx, original.Header().ID)
	if err != nil || len(reloaded.Events()) != 4 {
		t.Fatalf("takeover log=%v err=%v", reloaded, err)
	}
}

func TestJSONLPersistenceRefusesDifferentIdentityAndSameLengthLog(t *testing.T) {
	ctx := t.Context()
	persistence, err := NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := NewSession("snapshot-fence", "/workspace")
	if _, err := original.Append("turn/start", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := original.Append("turn/end", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	detach, err := persistence.Attach(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if err := detach(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"header", "events"} {
		t.Run(change, func(t *testing.T) {
			candidate, err := persistence.Load(ctx, original.Header().ID)
			if err != nil {
				t.Fatal(err)
			}
			if change == "header" {
				candidate.header.CWD = "/different-workspace"
			} else {
				candidate.events[0].Data = []byte(`{"turn":2}`)
				candidate.events[1].Data = []byte(`{"turn":2}`)
			}
			if closeCandidate, err := persistence.Attach(ctx, candidate); !errors.Is(err, ErrSessionPersistenceConflict) {
				if closeCandidate != nil {
					_ = closeCandidate()
				}
				t.Fatalf("conflicting %s accepted: %v", change, err)
			}
		})
	}
	loaded, err := persistence.Load(ctx, original.Header().ID)
	if err != nil {
		t.Fatal(err)
	}
	detach, err = persistence.Attach(ctx, loaded)
	if err != nil {
		t.Fatalf("conflict retained lease: %v", err)
	}
	if err := detach(); err != nil {
		t.Fatal(err)
	}
}

func TestJSONLWriteLeaseExcludesProcessAndReleasesAfterCrash(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestJSONLWriteLeaseHelper$")
	child.Env = append(os.Environ(), "POINT_TEST_JSONL_LEASE_ROOT="+dir)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	var diagnostics bytes.Buffer
	child.Stderr = &diagnostics
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "holding\n" {
		t.Fatalf("holder readiness=%q err=%v stderr=%s", line, err, &diagnostics)
	}
	persistence, err := NewJSONLSessionPersistence(dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := persistence.Load(ctx, "process-writer")
	if err != nil || len(loaded.Events()) != 2 {
		t.Fatalf("read during ownership: session=%v err=%v", loaded, err)
	}
	if detach, err := persistence.Attach(ctx, loaded); !errors.Is(err, ErrSessionAlreadyOwned) {
		if detach != nil {
			_ = detach()
		}
		t.Fatalf("cross-process write admitted: %v", err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	waited = true
	detach, err := persistence.Attach(ctx, loaded)
	if err != nil {
		t.Fatalf("crashed process retained lease: %v", err)
	}
	if _, err := loaded.Append("turn/start", map[string]int{"turn": 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.Append("turn/end", map[string]int{"turn": 2}); err != nil {
		t.Fatal(err)
	}
	if err := detach(); err != nil {
		t.Fatal(err)
	}
	readback, err := persistence.Load(ctx, "process-writer")
	if err != nil || len(readback.Events()) != 4 {
		t.Fatalf("takeover read=%v err=%v", readback, err)
	}
}

// Re-executed only by the process-ownership test. The parent kills this
// process, so no Go defer or persistence detach can release the lease for it.
func TestJSONLWriteLeaseHelper(t *testing.T) {
	dir := os.Getenv("POINT_TEST_JSONL_LEASE_ROOT")
	if dir == "" {
		return
	}
	persistence, err := NewJSONLSessionPersistence(dir)
	if err != nil {
		t.Fatal(err)
	}
	session := NewSession("process-writer", dir)
	detach, err := persistence.Attach(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	if _, err := session.Append("turn/start", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append("turn/end", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	if err := session.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "holding")
	var input [1]byte
	_, _ = os.Stdin.Read(input[:])
}

func TestJSONLPersistenceFlushAndDetachDrainCommittedObserverSuffix(t *testing.T) {
	for _, operation := range []string{"flush", "detach"} {
		t.Run(operation, func(t *testing.T) {
			persistence, err := NewJSONLSessionPersistence(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			session := NewSession(SessionID("observer-"+operation), "")
			var detach func() error
			var observerErr error
			var observedEvents int
			// The first observer runs before the persistence listener. Flush
			// and detach must still include the event already in the log.
			unsubscribe := session.Subscribe(func(Event) {
				if operation == "flush" {
					observerErr = session.Flush(t.Context())
				} else {
					observerErr = detach()
				}
				if observerErr != nil {
					return
				}
				loaded, loadErr := persistence.Load(t.Context(), session.Header().ID)
				observerErr = loadErr
				if loaded != nil {
					observedEvents = len(loaded.Events())
				}
			})
			defer unsubscribe()
			detach, err = persistence.Attach(t.Context(), session)
			if err != nil {
				t.Fatal(err)
			}
			defer detach()
			if _, err := session.Append("fact", map[string]string{"value": "committed"}); err != nil {
				t.Fatal(err)
			}
			if observerErr != nil || observedEvents != 1 {
				t.Fatalf("%s missed committed event: count=%d err=%v", operation, observedEvents, observerErr)
			}
			if err := detach(); err != nil {
				t.Fatal(err)
			}
			loaded, err := persistence.Load(t.Context(), session.Header().ID)
			if err != nil || len(loaded.Events()) != 1 {
				t.Fatalf("late notification duplicated or lost event: session=%v err=%v", loaded, err)
			}
		})
	}
}

func TestJSONLPersistenceLoadUsesAuthoritativeRestoreValidation(t *testing.T) {
	for _, scenario := range []string{"context-plan", "mixed-envelope", "empty-envelope"} {
		t.Run(scenario, func(t *testing.T) {
			persistence, err := NewJSONLSessionPersistence(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			header := NewSession(SessionID("invalid-"+scenario), "").Header()
			var events []Event
			switch scenario {
			case "context-plan":
				events = []Event{{Seq: 1, Type: "request/context-plan", Data: json.RawMessage(`{"kind":"forged","digest":"wrong","memoryTokens":-1,"memoryBudget":-1}`)}}
			}
			var body bytes.Buffer
			encoder := json.NewEncoder(&body)
			first := persistedLine{Header: &header}
			if scenario == "mixed-envelope" {
				first.Event = &Event{Seq: 1, Type: "fact", Data: json.RawMessage(`{}`)}
			}
			if err := encoder.Encode(first); err != nil {
				t.Fatal(err)
			}
			if scenario == "empty-envelope" {
				if err := encoder.Encode(persistedLine{}); err != nil {
					t.Fatal(err)
				}
			}
			for _, event := range events {
				if err := encoder.Encode(persistedLine{Event: &event}); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(persistence.path(header.ID), body.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := persistence.Load(t.Context(), header.ID); err == nil {
				t.Fatalf("accepted invalid %s session", scenario)
			}
		})
	}
}

func TestConcurrentAttachmentsOfExactSessionPublishOneOwner(t *testing.T) {
	p, err := NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := NewSession("shared-live-identity", t.TempDir())
	type result struct {
		detach func() error
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 16)
	for range 16 {
		go func() { <-start; detach, err := p.Attach(t.Context(), session); results <- result{detach, err} }()
	}
	close(start)
	winners := 0
	for range 16 {
		r := <-results
		if r.err == nil {
			winners++
			t.Cleanup(func() { _ = r.detach() })
		} else if !errors.Is(r.err, ErrSessionDurabilityAttached) {
			t.Errorf("same live identity misclassified: %v", r.err)
		}
	}
	if winners != 1 {
		t.Fatalf("owners=%d", winners)
	}
	if !session.DurabilityAttached() {
		t.Fatal("winning owner did not publish checkpoint")
	}
}

func TestPersistenceAdmitsOnlyValidatedRestoredSuffix(t *testing.T) {
	p, err := NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := NewSession("restore-suffix", t.TempDir())
	if _, err = original.Append("fixture/initial", map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	detach, err := p.Attach(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	if err = detach(); err != nil {
		t.Fatal(err)
	}
	restored, err := p.Load(t.Context(), original.Header().ID)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := p.Load(t.Context(), original.Header().ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Append("fixture/repair", map[string]int{"n": 2}); err != nil {
		t.Fatal(err)
	}
	detach, err = p.Attach(t.Context(), restored)
	if err != nil {
		t.Fatal(err)
	}
	// The pre-activation repair is durable before Attach publishes a checkpoint.
	loaded, err := p.Load(t.Context(), original.Header().ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Events()) != 2 {
		t.Fatalf("repair suffix not durable: %+v", loaded.Events())
	}
	if err = detach(); err != nil {
		t.Fatal(err)
	}
	if _, err = stale.Append("fixture/different", map[string]int{"n": 9}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Attach(t.Context(), stale); !errors.Is(err, ErrSessionPersistenceConflict) {
		t.Fatalf("stale conflicting suffix admitted: %v", err)
	}
	// A newly constructed identity has no restore frontier authorizing suffixes.
	if _, err = original.Append("fixture/forged", map[string]int{"n": 3}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Attach(t.Context(), original); !errors.Is(err, ErrSessionPersistenceConflict) {
		t.Fatalf("unrestored suffix admitted: %v", err)
	}
}
