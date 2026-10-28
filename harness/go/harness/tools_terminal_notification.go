package harness

import (
	"context"
	"log/slog"
	"sync"
)

// This is an observation receipt, never an execution or mutation receipt.
// Failure/uncertain delivery must not change an already executed outcome or
// replay body, spill, policy or cleanup. The transport owns diagnostics.
type toolTerminalNotification struct {
	once     sync.Once
	callback func(context.Context, ToolOutcome)
}

func (n *toolTerminalNotification) publish(ctx context.Context, outcome ToolOutcome) {
	n.once.Do(func() {
		defer func() {
			if recover() != nil {
				func() {
					defer func() { _ = recover() }()
					slog.Warn("harness: terminal source observation panicked; settled tool outcome retained, notification not retried")
				}()
			}
		}()
		detached := observerToolOutcome(outcome)
		detached.Request.Arguments = append([]byte(nil), outcome.Request.Arguments...)
		n.callback(ctx, detached)
	})
}

// UsesDSHTerminalNotification reports only the trusted, captured output receipt.
func (r ToolRequest) UsesDSHTerminalNotification() bool {
	return r.dshOutputProjection && r.output != nil && r.output.notification != nil
}
