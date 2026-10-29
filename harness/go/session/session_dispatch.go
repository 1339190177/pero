package session

import (
	"encoding/json"
	"errors"
	"fmt"
)

type codeDispatchEventData struct {
	RootCallID   string          `json:"rootCallId"`
	ParentCallID string          `json:"parentCallId"`
	SubCallID    string          `json:"subCallId"`
	Name         string          `json:"name"`
	Arguments    json.RawMessage `json:"arguments"`
	IsError      *bool           `json:"isError"`
	Content      json.RawMessage `json:"content"`
}

func decodeCodeDispatchEvent(event Event) (codeDispatchEventData, bool, error) {
	if event.Type != "tool/code-dispatch-start" && event.Type != "tool/code-dispatch" {
		return codeDispatchEventData{}, false, nil
	}
	var data codeDispatchEventData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return data, true, fmt.Errorf("harness: invalid %s payload: %w", event.Type, err)
	}
	if data.RootCallID == "" || data.ParentCallID == "" || data.SubCallID == "" || data.Name == "" || len(data.Arguments) == 0 {
		return data, true, fmt.Errorf("harness: %s must carry rootCallId, parentCallId, subCallId, name, and arguments", event.Type)
	}
	if event.Type == "tool/code-dispatch" {
		var content []ContentBlock
		if data.IsError == nil || len(data.Content) == 0 || json.Unmarshal(data.Content, &content) != nil {
			return data, true, errors.New("harness: tool/code-dispatch must carry isError and content")
		}
	}
	return data, true, nil
}

// codeDispatchIndex carries what validateCodeDispatchEvent derives from a whole
// prefix, one event at a time, so restoring a log does not rescan and re-decode
// the prefix for every dispatch event. It gives exactly the verdicts the prefix
// scan would: the first turn-order violation stays sticky until a dispatch event
// asks, and a later duplicate subCallId replaces an earlier root as before.
type codeDispatchIndex struct {
	roots   map[string]string
	open    *int
	turnErr error
}

func newCodeDispatchIndex() *codeDispatchIndex {
	return &codeDispatchIndex{roots: make(map[string]string)}
}

func (d *codeDispatchIndex) check(candidate Event, data codeDispatchEventData) error {
	if d.turnErr != nil {
		return d.turnErr
	}
	if d.open == nil {
		return fmt.Errorf("harness: %s appended outside any open turn", candidate.Type)
	}
	return checkCodeDispatchRoots(candidate, data, d.roots)
}

func (d *codeDispatchIndex) record(event Event, data codeDispatchEventData, dispatch bool) {
	if dispatch {
		d.roots[data.SubCallID] = data.RootCallID
	}
	if d.turnErr != nil || event.Type != "turn/start" && event.Type != "turn/end" {
		return
	}
	var payload struct {
		Turn int `json:"turn"`
	}
	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.Turn <= 0 {
		d.turnErr = fmt.Errorf("harness: invalid %s at seq %d", event.Type, event.Seq)
		return
	}
	if event.Type == "turn/start" {
		if d.open != nil {
			d.turnErr = fmt.Errorf("harness: turn/start at seq %d while turn %d is still open", event.Seq, *d.open)
			return
		}
		d.open = &payload.Turn
		return
	}
	if d.open == nil || *d.open != payload.Turn {
		d.turnErr = fmt.Errorf("harness: turn/end at seq %d does not match an open turn", event.Seq)
		return
	}
	d.open = nil
}

func checkCodeDispatchRoots(candidate Event, data codeDispatchEventData, roots map[string]string) error {
	if known := roots[data.SubCallID]; known != "" && known != data.RootCallID {
		return fmt.Errorf("harness: %s changed rootCallId for subCallId %s", candidate.Type, data.SubCallID)
	}
	if data.ParentCallID != data.RootCallID && roots[data.ParentCallID] != data.RootCallID {
		return fmt.Errorf("harness: %s parentCallId %s does not belong to rootCallId %s", candidate.Type, data.ParentCallID, data.RootCallID)
	}
	return nil
}

func validateCodeDispatchEvent(events []Event, candidate Event) error {
	data, dispatch, err := decodeCodeDispatchEvent(candidate)
	if err != nil || !dispatch {
		return err
	}
	openTurn, err := inspectOpenTurn(events)
	if err != nil {
		return err
	}
	if openTurn == nil {
		return fmt.Errorf("harness: %s appended outside any open turn", candidate.Type)
	}
	roots := make(map[string]string)
	for _, event := range events {
		prior, ok, decodeErr := decodeCodeDispatchEvent(event)
		if decodeErr != nil {
			return decodeErr
		}
		if ok {
			roots[prior.SubCallID] = prior.RootCallID
		}
	}
	return checkCodeDispatchRoots(candidate, data, roots)
}
