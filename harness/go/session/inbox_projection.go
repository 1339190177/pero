package session

import (
	"encoding/json"
	"fmt"
)

// InboxProjectionState preserves the complete durable message JSON, including
// future fields unknown to the native driver. It is also the public wire view.
type InboxProjectionState map[InboxTarget][]json.RawMessage

// The persisted fold retains the delegated seed boundary separately from the
// unchanged public inbox view, so cold replay never executes ancestor input.
type inboxProjectionState struct {
	Pending    *InboxProjectionState `json:"pending"`
	SeedLength uint64                `json:"seedLength"`
}

func InboxProjectionDefinition() ProjectionDefinition {
	d := DefineProjection(ProjectionSpec[*inboxProjectionState, *InboxProjectionState]{
		// Version 3 excludes executable input inherited by delegated forks.
		Key: "inbox", StateVersion: 3,
		Init: func(header SessionHeader) *inboxProjectionState {
			s := InboxProjectionState{InboxNextTurn: {}, InboxNextStep: {}}
			return &inboxProjectionState{Pending: &s, SeedLength: executableInboxSeedLength(header)}
		},
		Apply: func(s *inboxProjectionState, _ Event) *inboxProjectionState { return s },
		View:  func(s *inboxProjectionState) *InboxProjectionState { return s.Pending },
		ValidateState: func(s *inboxProjectionState) error {
			if s == nil {
				return fmt.Errorf("inbox: missing projection state")
			}
			return validateInboxProjection(s.Pending)
		},
		ValidateView: validateInboxProjection,
	})
	d.Apply = func(raw any, event Event) (any, error) {
		state, ok := raw.(*inboxProjectionState)
		if !ok || state == nil || state.Pending == nil {
			return nil, fmt.Errorf("inbox: invalid projection state")
		}
		if event.Seq <= state.SeedLength && state.SeedLength != 0 {
			return state, nil
		}
		next, err := applyInboxProjection(state.Pending, event)
		if err != nil || next == state.Pending {
			return state, err
		}
		return &inboxProjectionState{Pending: next, SeedLength: state.SeedLength}, nil
	}
	return d
}

func applyInboxProjection(s *InboxProjectionState, event Event) (*InboxProjectionState, error) {
	if event.Type == "agent/inbox/moved" {
		return moveInboxProjection(s, event)
	}
	if event.Type != "agent/inbox/spliced" {
		return s, nil
	}
	var splice struct {
		Target       InboxTarget       `json:"target"`
		Start        int               `json:"start"`
		RemovedCount int               `json:"removedCount"`
		Inserted     []json.RawMessage `json:"inserted"`
	}
	fail := func(err error) (*InboxProjectionState, error) {
		return nil, fmt.Errorf("invalid persisted inbox splice at session seq %d: %w", event.Seq, err)
	}
	if err := json.Unmarshal(event.Data, &splice); err != nil {
		return fail(err)
	}
	list, ok := (*s)[splice.Target]
	if !ok || splice.Start < 0 || splice.Start > len(list) || splice.RemovedCount < 0 || splice.RemovedCount > len(list)-splice.Start {
		return fail(fmt.Errorf("invalid inbox splice"))
	}
	next := InboxProjectionState{InboxNextTurn: (*s)[InboxNextTurn], InboxNextStep: (*s)[InboxNextStep]}
	updated := make([]json.RawMessage, 0, len(list)-splice.RemovedCount+len(splice.Inserted))
	updated = append(updated, list[:splice.Start]...)
	updated = append(updated, splice.Inserted...)
	updated = append(updated, list[splice.Start+splice.RemovedCount:]...)
	next[splice.Target] = updated
	if err := validateInboxProjection(&next); err != nil {
		return fail(err)
	}
	return &next, nil
}

func validateInboxProjection(s *InboxProjectionState) error {
	if s == nil {
		return fmt.Errorf("inbox: missing state")
	}
	ids := map[string]bool{}
	for _, target := range []InboxTarget{InboxNextTurn, InboxNextStep} {
		list, ok := (*s)[target]
		if !ok || list == nil {
			return fmt.Errorf("inbox: missing %s list", target)
		}
		for _, raw := range list {
			var identity struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &identity); err != nil {
				return err
			}
			if ids[identity.ID] {
				return fmt.Errorf("message %q is already pending", identity.ID)
			}
			ids[identity.ID] = true
		}
	}
	return nil
}

// moveInboxProjection changes both queues in one candidate snapshot. Keeping
// the original RawMessage retains fields a newer producer may have added.
func moveInboxProjection(s *InboxProjectionState, event Event) (*InboxProjectionState, error) {
	fail := func(err error) (*InboxProjectionState, error) {
		return nil, fmt.Errorf("invalid persisted inbox move at session seq %d: %w", event.Seq, err)
	}
	var move struct {
		ID     string      `json:"id"`
		Target InboxTarget `json:"target"`
	}
	if err := json.Unmarshal(event.Data, &move); err != nil {
		return fail(err)
	}
	if move.ID == "" || (move.Target != InboxNextTurn && move.Target != InboxNextStep) {
		return fail(fmt.Errorf("invalid inbox move"))
	}
	if err := validateInboxProjection(s); err != nil {
		return fail(err)
	}
	for _, from := range []InboxTarget{InboxNextTurn, InboxNextStep} {
		for index, raw := range (*s)[from] {
			var identity struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &identity); err != nil {
				return fail(err)
			}
			if identity.ID != move.ID {
				continue
			}
			if from == move.Target {
				return s, nil
			}
			list := (*s)[from]
			next := InboxProjectionState{}
			next[from] = make([]json.RawMessage, 0, len(list)-1)
			next[from] = append(next[from], list[:index]...)
			next[from] = append(next[from], list[index+1:]...)
			next[move.Target] = make([]json.RawMessage, 0, len((*s)[move.Target])+1)
			next[move.Target] = append(next[move.Target], (*s)[move.Target]...)
			next[move.Target] = append(next[move.Target], raw)
			if err := validateInboxProjection(&next); err != nil {
				return fail(err)
			}
			return &next, nil
		}
	}
	return fail(fmt.Errorf("pending message %q not found", move.ID))
}
