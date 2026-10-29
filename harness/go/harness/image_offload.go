package harness

import (
	"context"
	"errors"

	point "github.com/phodal/pero/harness/go/runtime"
)

// MountImageOffload records durable image omissions when an image-capable
// route rejects a request, then retries that request without spending the
// provider retry budget or writing an llm/retry event.
func MountImageOffload(run *point.Run, agent *Agent) error {
	if run == nil || !run.Active() {
		return point.ErrInactiveRun
	}
	if agent == nil || agent.session == nil {
		return errors.New("harness: image offload requires an agent session")
	}
	_, err := agent.hooks.RequestError.Use(run, func(ctx context.Context, input RequestErrorInput, next point.Next[RequestErrorInput]) (RequestErrorInput, error) {
		if input.Agent != agent || input.Failure.Code != ImageOffloadRequiredCode || input.Failure.OffloadImages == nil {
			return next(input)
		}
		if err := ctx.Err(); err != nil {
			return input, err
		}
		recorded, err := agent.session.OffloadOldestImages(agent.session.SurfaceSeqs(), *input.Failure.OffloadImages)
		if err != nil {
			return input, err
		}
		if !recorded {
			return next(input)
		}
		input.Retry = true
		return input, nil
	}, point.ListenerOptions{Prepend: true})
	return err
}
