package harness

import (
	"context"
	"errors"
)

// AdmitUser commits a human-authored prompt to the Agent inbox before waking
// its worker. The caller's context bounds admission, not the accepted turn.
func (a *Agent) AdmitUser(ctx context.Context, message Message, target InboxTarget) (Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if target != InboxNextTurn && target != InboxNextStep {
		return Message{}, errors.New("invalid user inbox target")
	}
	message.Role = RoleUser
	prepared, err := a.prepareQueuedMessage(ctx, message, true)
	if err != nil {
		return Message{}, err
	}
	if !messageHasUserSource(prepared) {
		return Message{}, errors.New("user admission requires user source")
	}
	a.inboxClaimMu.Lock()
	if err := a.mutationAdmissionError(); err != nil {
		a.inboxClaimMu.Unlock()
		return Message{}, err
	}
	select {
	case <-a.done:
		a.inboxClaimMu.Unlock()
		return Message{}, errors.New("agent is disposed")
	default:
	}
	inserted := false
	if err = ctx.Err(); err == nil {
		inserted, err = a.durableInbox.Append(target, prepared)
	}
	if err == nil && a.session.DurabilityAttached() {
		err = a.session.Flush(ctx)
	}
	a.inboxClaimMu.Unlock()
	if err != nil {
		return Message{}, err
	}
	if inserted {
		a.publishInboxInserted(prepared, target, true)
	}
	// Queue a coalesced wake even during a turn: its final-step check may
	// have raced this admission. An already claimed message makes the wake empty.
	a.requestDurableWakeFor(target)
	return prepared, nil
}

// EditUserInbox identifies the pending message under the same lock as claims;
// a consumed message cannot accidentally turn into an edit of its successor.
func (a *Agent) EditUserInbox(ctx context.Context, id string, content []ContentBlock, remove bool) error {
	if err := a.mutationAdmissionError(); err != nil {
		return err
	}
	a.inboxClaimMu.Lock()
	if err := a.mutationAdmissionError(); err != nil {
		a.inboxClaimMu.Unlock()
		return err
	}
	var old Message
	for _, target := range []InboxTarget{InboxNextTurn, InboxNextStep} {
		for _, m := range a.durableInbox.Pending(target) {
			if m.ID == id {
				old = m
			}
		}
	}
	if old.ID == "" || !messageHasUserSource(old) {
		a.inboxClaimMu.Unlock()
		return errors.New("pending user message not found")
	}
	var mutation InboxMutation
	var err error
	if remove {
		var found bool
		mutation, found, err = a.durableInbox.Remove(id)
		if err == nil && !found {
			err = errors.New("pending message was consumed")
		}
	} else {
		replacement := Message{Role: RoleUser, Source: old.Source, Content: content}
		replacement, err = a.prepareQueuedMessage(ctx, replacement, true)
		if err == nil {
			var found bool
			mutation, found, err = a.durableInbox.Replace(id, replacement)
			if err == nil && !found {
				err = errors.New("pending message was consumed")
			}
		}
	}
	if err == nil && a.session.DurabilityAttached() {
		err = a.session.Flush(ctx)
	}
	a.inboxClaimMu.Unlock()
	if err != nil {
		return err
	}
	for _, m := range mutation.Removed {
		a.publishInboxDiscarded(m, true)
	}
	return nil
}

func (a *Agent) SteerUserInbox(ctx context.Context, id string) error {
	if err := a.mutationAdmissionError(); err != nil {
		return err
	}
	a.inboxClaimMu.Lock()
	if err := a.mutationAdmissionError(); err != nil {
		a.inboxClaimMu.Unlock()
		return err
	}
	var found bool
	for _, m := range a.durableInbox.Pending(InboxNextTurn) {
		if m.ID == id && messageHasUserSource(m) {
			found = true
		}
	}
	if !found {
		a.inboxClaimMu.Unlock()
		return errors.New("queued user message not found")
	}
	a.mu.Lock()
	interrupt := a.inboxInterrupt
	a.mu.Unlock()
	err := a.durableInbox.Move(id, InboxNextStep)
	if err == nil && a.session.DurabilityAttached() {
		err = a.session.Flush(ctx)
	}
	a.inboxClaimMu.Unlock()
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.inboxInterrupt == interrupt {
		a.inboxParked = false
	}
	a.mu.Unlock()
	a.requestDurableWakeFor(InboxNextStep)
	return nil
}

func (a *Agent) inboxClaimUnlock(_ []Message) { a.inboxClaimMu.Unlock() }
