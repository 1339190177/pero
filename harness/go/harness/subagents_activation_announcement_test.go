// Adapted from subagents_activation_test.go: declarations split by responsibility without behavior changes.
package harness

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	point "github.com/phodal/pero/harness/go/runtime"
)

func TestSubagentActivationAdoptsPreparationFromActivatedObserver(t *testing.T) {
	ctx := activationTestContext(t)
	h, err := Boot(ctx, Options{EnableSubagents: true, Adapter: AdapterFunc(func(ctx context.Context, _ GenerateRequest, _ func(StreamChunk) error) (Completion, error) {
		<-ctx.Done()
		return Completion{}, ctx.Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Dispose(context.Background())
	parent, err := h.Agents().Create(ctx, AgentOptions{ID: "prebind-root", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	finished := make(chan error, 1)
	external := activationExternalProvider{retirementSubagentProvider{prepare: func(ctx context.Context, _ SubagentRequest) (SubagentProviderRun, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return SubagentProviderRun{}, ctx.Err()
	}}}
	local := activationLocalTestProvider(h, "prebind-parent", nil)
	mountRetirementHooks(t, h, func(run *point.Run) error {
		if _, err := h.Subagents().RegisterProvider(run, local); err != nil {
			return err
		}
		if _, err := h.Subagents().RegisterProvider(run, external); err != nil {
			return err
		}
		_, err := h.Hooks().Activated.On(run, func(_ context.Context, agent *Agent) error {
			if agent.ID() != "prebind-parent" {
				return nil
			}
			go func() {
				_, err := h.Subagents().StartActivation(ctx, external.Name(), SubagentRequest{Parent: agent, Prompt: "grandchild preparation", MaxDepth: 3}, "caller")
				finished <- err
			}()
			select {
			case <-entered:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		return err
	})
	activation, err := h.Subagents().StartActivation(ctx, local.Name(), SubagentRequest{Parent: parent, Prompt: "work", MaxDepth: 3}, "caller")
	if err != nil {
		t.Fatal(err)
	}
	_ = activation.RequestDispose()
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("pre-bind descendant did not inherit cancellation")
	}
	select {
	case <-activation.Done:
		t.Fatal("parent receipt preceded pending descendant preparation rollback")
	default:
	}
	unblock()
	if err := activationReceive(t, ctx, finished); !errors.Is(err, context.Canceled) {
		t.Fatalf("late preparation=%v", err)
	}
	if err := activation.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSubagentActivationTerminalWaitsForInitialAnnouncement(t *testing.T) {
	ctx := activationTestContext(t)
	h := subagentTestHarness(t)
	parent, err := h.Agents().Create(ctx, AgentOptions{ID: "announcement-root", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	ended := make(chan struct{}, 1)
	provider := retirementSubagentProvider{prepare: func(context.Context, SubagentRequest) (SubagentProviderRun, error) {
		return SubagentProviderRun{ChildID: "announcement-child", Result: make(chan SubagentResult), Begin: func() { t.Error("withdrawn child began") }, Cancel: func(error) {}, Dispose: func(context.Context) error { return nil }}, nil
	}}
	mountRetirementHooks(t, h, func(run *point.Run) error {
		if _, err := h.Subagents().RegisterProvider(run, provider); err != nil {
			return err
		}
		if _, err := h.Subagents().Started.On(run, func(context.Context, SubagentLifecycle) error { close(entered); <-release; return nil }); err != nil {
			return err
		}
		_, err := h.Subagents().Ended.On(run, func(context.Context, SubagentLifecycle) error { ended <- struct{}{}; return nil })
		return err
	})
	finished := make(chan error, 1)
	go func() {
		_, err := h.Subagents().StartActivation(ctx, provider.Name(), SubagentRequest{Parent: parent, Prompt: "work", MaxDepth: 2}, "caller")
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	r := h.Subagents()
	r.mu.RLock()
	record := r.records["announcement-child"]
	r.mu.RUnlock()
	if err := r.requestActivationDisposal(record); !errors.Is(err, ErrSubagentActivationDisposalPending) {
		t.Fatal(err)
	}
	select {
	case <-record.activation.cleanupDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-record.activation.done:
		t.Fatal("terminal result preceded initial admission receipt")
	case <-ended:
		t.Fatal("Ended raced ahead of the Started callback")
	default:
	}
	unblock()
	if err := activationReceive(t, ctx, finished); err == nil {
		t.Fatal("withdrawn initial admission succeeded")
	}
	if err := record.activation.wait(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	default:
		t.Fatal("initial Started did not receive its terminal edge")
	}
}

func TestSubagentActivationResidentMessageCannotOwnInitialAnnouncement(t *testing.T) {
	ctx := activationTestContext(t)
	h, err := Boot(ctx, Options{EnableSubagents: true, Adapter: AdapterFunc(func(ctx context.Context, _ GenerateRequest, _ func(StreamChunk) error) (Completion, error) {
		<-ctx.Done()
		return Completion{}, ctx.Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Dispose(context.Background())
	parent, err := h.Agents().Create(ctx, AgentOptions{ID: "initial-owner-root", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var starts atomic.Int32
	provider := activationLocalTestProvider(h, "initial-owner-child", nil)
	mountRetirementHooks(t, h, func(run *point.Run) error {
		if _, err := h.Subagents().RegisterProvider(run, provider); err != nil {
			return err
		}
		_, err := h.Subagents().Started.On(run, func(_ context.Context, info SubagentLifecycle) error {
			if info.ID != "initial-owner-child" || !info.Managed {
				t.Error("resident input emitted a non-managed or foreign Started event")
			}
			if starts.Add(1) == 1 {
				close(entered)
				<-release
			}
			return nil
		})
		return err
	})
	finished := make(chan error, 1)
	go func() {
		_, err := h.Subagents().StartActivation(ctx, provider.Name(), SubagentRequest{Parent: parent, Prompt: "initial", MaxDepth: 2}, "caller")
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	r := h.Subagents()
	r.mu.RLock()
	record := r.records["initial-owner-child"]
	r.mu.RUnlock()
	// Model the narrow published-but-unbound registry state independently of
	// scheduler timing. Premature input must reject without waiting on startup.
	record.activation.bindingReady.Store(false)
	before := len(record.child.Session().Events())
	if _, err := r.SendMessage(ctx, parent, "initial-owner-child", []ContentBlock{{Type: BlockText, Text: "premature message"}}); !errors.Is(err, ErrSubagentActivationNotReady) {
		t.Fatalf("unbound input receipt=%v", err)
	}
	if len(record.child.Session().Events()) != before {
		t.Fatal("unbound input mutated the child")
	}
	record.activation.bindingReady.Store(true)
	if _, err := r.SendMessage(ctx, parent, "initial-owner-child", []ContentBlock{{Type: BlockText, Text: "concurrent message"}}); err != nil {
		t.Fatal(err)
	}
	if id, err := r.FollowupMessage(ctx, parent, "initial-owner-child", Message{ID: "legacy-resident-input", Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: "legacy concurrent message"}}}); err != nil || id != "legacy-resident-input" {
		t.Fatalf("managed legacy input receipt=%q error=%v", id, err)
	}
	if starts.Load() != 1 {
		t.Fatalf("resident current/legacy input emitted duplicate Started: %d", starts.Load())
	}
	_ = r.requestActivationDisposal(record)
	select {
	case <-record.activation.cleanupDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-record.activation.initialDone:
		t.Fatal("resident caller closed original admission barrier")
	case <-record.activation.done:
		t.Fatal("resident caller allowed premature completion")
	default:
	}
	unblock()
	if err := activationReceive(t, ctx, finished); err == nil {
		t.Fatal("withdrawn initial admission succeeded")
	}
	if err := record.activation.wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSubagentActivationAdoptsWithdrawnChildFinalReceipt(t *testing.T) {
	ctx := activationTestContext(t)
	h, err := Boot(ctx, Options{EnableSubagents: true, Adapter: AdapterFunc(func(ctx context.Context, _ GenerateRequest, _ func(StreamChunk) error) (Completion, error) {
		<-ctx.Done()
		return Completion{}, ctx.Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Dispose(context.Background())
	parent, err := h.Agents().Create(ctx, AgentOptions{ID: "prebind-cleanup-root", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	entered, release, deactivated := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	terminal := make(chan SubagentResult, 1)
	ready := make(chan SubagentActivation, 1)
	external := activationExternalProvider{retirementSubagentProvider{prepare: func(context.Context, SubagentRequest) (SubagentProviderRun, error) {
		return SubagentProviderRun{ChildID: "prebind-cleanup-external", Result: terminal, Begin: func() {}, Cancel: func(error) {}, Dispose: func(context.Context) error { close(entered); <-release; return nil }}, nil
	}}}
	local := activationLocalTestProvider(h, "prebind-cleanup-parent", nil)
	mountRetirementHooks(t, h, func(run *point.Run) error {
		if _, err := h.Subagents().RegisterProvider(run, local); err != nil {
			return err
		}
		if _, err := h.Subagents().RegisterProvider(run, external); err != nil {
			return err
		}
		if _, err := h.Hooks().Deactivated.On(run, func(_ context.Context, agent *Agent) error {
			if agent.ID() == "prebind-cleanup-parent" {
				close(deactivated)
			}
			return nil
		}); err != nil {
			return err
		}
		_, err := h.Hooks().Activated.On(run, func(_ context.Context, agent *Agent) error {
			if agent.ID() != "prebind-cleanup-parent" {
				return nil
			}
			activation, err := h.Subagents().StartActivation(ctx, external.Name(), SubagentRequest{Parent: agent, Prompt: "external", MaxDepth: 3}, "caller")
			if err != nil {
				return err
			}
			terminal <- SubagentResult{ChildID: activation.ChildID, Status: SubagentCompleted, Reason: FinishStop}
			close(terminal)
			select {
			case <-entered:
				ready <- activation
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		return err
	})
	outer, err := h.Subagents().StartActivation(ctx, local.Name(), SubagentRequest{Parent: parent, Prompt: "work", MaxDepth: 3}, "caller")
	if err != nil {
		t.Fatal(err)
	}
	inner := activationReceive(t, ctx, ready)
	_ = outer.RequestDispose()
	select {
	case <-deactivated:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-outer.Done:
		t.Fatal("parent completed before adopted external cleanup receipt")
	default:
	}
	unblock()
	if err := outer.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := inner.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}
