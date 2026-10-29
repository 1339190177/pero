package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	harnesssession "github.com/phodal/pero/harness/go/session"
)

// FollowupAdmission is a framework-only native producer receipt. It is never
// exposed through profile configuration or guest transports. The Agent binds it
// to its exact incarnation and prepared message, independently of Source.kind.
// Check may reject before/after policy preparation. Commit must validate its
// own live authority and invoke commit only while that authority is reserved.
// commit atomically appends facts and the Agent's immutable accepted batch;
// a CAS miss may be retried after revalidation. Check and Commit run without
// Agent/inbox locks. Discard is a bounded framework ownership release: it may
// run under existing Agent admission locks, must not call Agent, Session/inbox,
// listeners or user code, and must not wait. It is not a general callback hook.
type FollowupAdmission struct {
	Producer string
	Lifetime context.Context
	Check    func(context.Context, Message) (bool, error)
	Commit   func(context.Context, Message, func(uint64, []harnesssession.AdmissionFact) error) (bool, error)
	Discard  func()
}

type followupAdmissionReceipt struct {
	owned     *OwnedFollowupReceipt
	message   Message
	digest    string
	admission FollowupAdmission
}

type guardedInboxFact struct {
	MessageID string `json:"messageId"`
	Digest    string `json:"digest"`
	Producer  string `json:"producer"`
}

type followupRecoveryKey struct{}
type followupRecoveryBatch struct {
	agent    *Agent
	messages []Message
}

func (a *Agent) withFollowupRecovery(ctx context.Context, messages []Message) context.Context {
	return context.WithValue(ctx, followupRecoveryKey{}, followupRecoveryBatch{a, cloneMessages(messages)})
}

func (a *Agent) followupRecoveryMessages(ctx context.Context, messages []Message, guarded Message) []Message {
	if original, ok := ctx.Value(followupRecoveryKey{}).(followupRecoveryBatch); ok && original.agent == a {
		for _, candidate := range original.messages {
			if candidate.ID == guarded.ID && followupDigest(candidate) == followupDigest(guarded) {
				return cloneMessages(original.messages)
			}
		}
	}
	return messages
}

var errFollowupAdmissionDropped = errors.New("harness: native followup admission withdrawn before dispatch")

// TryFollowupWithAdmission adds exact native ownership checks to TryFollowup.
// An intent fact is persisted before enqueue. It is a requirement for live
// authority, never proof of acceptance. A rejected enqueue may leave only that
// harmless fact. Restored pending input bearing it fails closed without a new
// live receipt; unrelated JS or native plugin input is unaffected.
func (a *Agent) TryFollowupWithAdmission(message Message, admission FollowupAdmission) (bool, error) {
	if admission.Producer == "" || admission.Lifetime == nil || admission.Check == nil || admission.Commit == nil {
		return false, errors.New("harness: incomplete native followup admission")
	}
	return a.tryFollowup(message, &admission)
}

func followupDigest(message Message) string {
	data, _ := json.Marshal(message)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (a *Agent) prepareFollowupAdmission(message Message, admission FollowupAdmission) (*followupAdmissionReceipt, error) {
	return a.prepareOwnedFollowupAdmission(message, admission, nil)
}

func (a *Agent) prepareOwnedFollowupAdmission(message Message, admission FollowupAdmission, owned *OwnedFollowupReceipt) (*followupAdmissionReceipt, error) {
	if err := admission.Lifetime.Err(); err != nil {
		return nil, err
	}
	receipt := &followupAdmissionReceipt{message: cloneMessage(message), digest: followupDigest(message), admission: admission, owned: owned}
	a.followupAdmissionMu.Lock()
	if a.followupAdmissions == nil {
		a.followupAdmissions = make(map[string]*followupAdmissionReceipt)
	}
	if _, exists := a.followupAdmissions[message.ID]; exists {
		a.followupAdmissionMu.Unlock()
		return nil, errors.New("harness: followup identity already guarded")
	}
	a.followupAdmissions[message.ID] = receipt
	a.followupAdmissionMu.Unlock()
	if _, err := a.session.Append("agent/inbox/guarded", guardedInboxFact{message.ID, receipt.digest, admission.Producer}); err != nil {
		a.abandonFollowupAdmission(message)
		return nil, err
	}
	return receipt, nil
}

func (a *Agent) followupReceipt(message Message) (*followupAdmissionReceipt, bool) {
	a.followupAdmissionMu.Lock()
	receipt, exists := a.followupAdmissions[message.ID]
	a.followupAdmissionMu.Unlock()
	if exists {
		return receipt, true
	}
	// A persisted requirement is scoped to an exact assigned ID, not a guessed
	// source kind. The digest is checked by live receipts, and cannot be used to
	// evade the requirement by rewriting bytes after resume.
	for _, event := range a.session.Events() {
		if event.Type != "agent/inbox/guarded" {
			continue
		}
		var fact guardedInboxFact
		if json.Unmarshal(event.Data, &fact) == nil && fact.MessageID == message.ID {
			return nil, true
		}
	}
	return nil, false
}

func (a *Agent) dropFollowupAdmission(message Message) {
	a.releaseFollowupAdmission(message, true)
}

func (a *Agent) abandonFollowupAdmission(message Message) {
	a.releaseFollowupAdmission(message, false)
}

func (a *Agent) releaseFollowupAdmission(message Message, notify bool) {
	a.followupAdmissionMu.Lock()
	receipt, seen := a.followupAdmissions[message.ID]
	if a.followupAdmissions == nil {
		a.followupAdmissions = make(map[string]*followupAdmissionReceipt)
	}
	a.followupAdmissions[message.ID] = nil
	a.followupAdmissionMu.Unlock()
	if seen && receipt == nil {
		return
	}
	if receipt != nil && receipt.admission.Discard != nil {
		func() { defer func() { _ = recover() }(); receipt.admission.Discard() }()
	}
	if notify {
		a.publishInboxDiscarded(message, false)
	}
}

func checkFollowupReceipt(ctx context.Context, receipt *followupAdmissionReceipt, message Message) (allowed bool, err error) {
	if receipt == nil || receipt.digest != followupDigest(message) || receipt.admission.Lifetime.Err() != nil || ctx.Err() != nil || !receipt.owned.ownsContext(ctx) {
		return false, nil
	}
	defer func() {
		if p := recover(); p != nil {
			allowed = false
			err = fmt.Errorf("harness: native admission check panicked: %v", p)
		}
	}()
	return receipt.admission.Check(ctx, cloneMessage(message))
}

func (a *Agent) filterFollowupAdmissions(ctx context.Context, messages []Message, recovery bool) []Message {
	result := make([]Message, 0, len(messages))
	for _, message := range messages {
		receipt, guarded := a.followupReceipt(message)
		if guarded {
			allowed := false
			if !recovery {
				allowed, _ = checkFollowupReceipt(ctx, receipt, message)
			}
			if !allowed {
				a.dropFollowupAdmission(message)
				continue
			}
		}
		result = append(result, message)
	}
	return result
}

// Policy rewrites cannot transplant authority by changing IDs or dropping an
// original guarded message and returning different bytes. Unrelated original
// inputs are restored rather than being lost with the rejected native round.
func (a *Agent) checkFollowupRewrite(ctx context.Context, original, rewritten []Message) bool {
	for _, message := range original {
		receipt, guarded := a.followupReceipt(message)
		if !guarded {
			continue
		}
		matches := 0
		for _, candidate := range rewritten {
			if followupDigest(candidate) == followupDigest(message) {
				matches++
			}
		}
		allowed, _ := checkFollowupReceipt(ctx, receipt, message)
		if matches != 1 || !allowed {
			return false
		}
	}
	return true
}

func (a *Agent) restoreUnrelatedAdmission(messages []Message) error {
	ordinary := a.filterFollowupAdmissions(context.Background(), messages, true)
	if len(ordinary) == 0 {
		return nil
	}
	// Preserve the original order ahead of later next-step work. This is reached
	// after claim locks have been released and never invokes native callbacks
	// while holding the Agent's inbox/admission locks.
	_, err := a.durableInbox.Splice(InboxNextStep, 0, 0, ordinary)
	return err
}

func (a *Agent) commitGuardedAdmission(ctx context.Context, messages []Message) (handled bool, resultErr error) {
	var receipt *followupAdmissionReceipt
	var guarded Message
	count := 0
	for _, message := range messages {
		r, ok := a.followupReceipt(message)
		if ok {
			count++
			receipt = r
			guarded = message
		}
	}
	if count == 0 {
		return false, nil
	}
	fail := func(err error) (bool, error) {
		restoreErr := a.restoreUnrelatedAdmission(a.followupRecoveryMessages(ctx, messages, guarded))
		if err == nil && restoreErr == nil {
			return true, errFollowupAdmissionDropped
		}
		return true, errors.Join(errFollowupAdmissionDropped, err, restoreErr)
	}
	if count != 1 {
		return fail(errors.New("harness: multiple native admission receipts in one batch"))
	}
	allowed, err := checkFollowupReceipt(ctx, receipt, guarded)
	if err != nil || !allowed {
		return fail(err)
	}
	committed := false
	defer func() {
		if committed {
			a.followupAdmissionMu.Lock()
			delete(a.followupAdmissions, guarded.ID)
			a.followupAdmissionMu.Unlock()
		}
		if p := recover(); p != nil {
			handled = true
			resultErr = fmt.Errorf("harness: native admission commit panicked: %v", p)
			if !committed {
				_, resultErr = fail(resultErr)
			}
		}
	}()
	accepted, err := receipt.admission.Commit(ctx, cloneMessage(guarded), func(seq uint64, facts []harnesssession.AdmissionFact) error {
		if committed {
			return errors.New("harness: native admission was already committed")
		}
		if receipt.admission.Lifetime.Err() != nil {
			return errors.New("harness: native admission owner retired")
		}
		events, commitErr := a.session.AppendUserAdmissionCAS(ctx, seq, facts, messages, receipt.admission.Lifetime)
		if len(events) > 0 {
			committed = true
		}
		return commitErr
	})
	if !committed {
		return fail(err)
	}
	a.followupAdmissionMu.Lock()
	delete(a.followupAdmissions, guarded.ID)
	a.followupAdmissionMu.Unlock()
	if err != nil {
		return true, err
	}
	if !accepted {
		return true, errors.New("harness: native admission owner rejected an already committed batch")
	}
	return true, nil
}
