package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestRetryExecutionPolicyGenerationAndOverrideDisposal(t *testing.T) {
	p := NewRetryExecutionPolicy()
	enabled, first := p.Capture()
	if !enabled || first.Err() != nil {
		t.Fatal("initial generation disabled")
	}
	restoreFirst, err := p.Override(false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Err() == nil || p.Enabled() {
		t.Fatal("disable did not cancel previous generation")
	}
	_, disabled := p.Capture()
	restoreSecond, err := p.Override(true)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Err() == nil || !p.Enabled() {
		t.Fatal("enable did not replace generation")
	}
	// Disposing a covered override must not replace the active generation.
	_, current := p.Capture()
	if err := restoreFirst(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current.Err() != nil || !p.Snapshot().Overridden {
		t.Fatal("covered disposal changed active policy")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = restoreSecond(context.Background()) }()
	}
	wg.Wait()
	if got := p.Snapshot(); got.Overridden || !got.Enabled {
		t.Fatalf("default not restored: %+v", got)
	}
	if current.Err() != nil {
		t.Fatal("unchanged enablement replaced generation")
	}
}

func TestProviderFailureRetainsCauseAndExplicitOffloadZero(t *testing.T) {
	cause := errors.New("transport")
	err := NewProviderError("", TransportErrorCode, cause)
	if !errors.Is(err, cause) {
		t.Fatal("cause lost")
	}
	failure := ProviderFailureFromError(err)
	if failure.Code != TransportErrorCode || failure.Message != cause.Error() {
		t.Fatalf("failure: %+v", failure)
	}
	offload := NewImageOffloadRequired(0)
	if offload.Failure.OffloadImages == nil || *offload.Failure.OffloadImages != 0 {
		t.Fatal("explicit zero count lost")
	}
	if IsContextWindowExceededDetail("HTTP 400 invalid request") {
		t.Fatal("generic error became context overflow")
	}
	if !IsContextWindowExceededDetail("maximum context length exceeded") {
		t.Fatal("explicit context overflow lost")
	}
}
