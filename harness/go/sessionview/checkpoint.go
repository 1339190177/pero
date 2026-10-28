package sessionview

import (
	"encoding/json"
	"fmt"
)

// Ordered objects retain their keys across checkpoint round trips.
func (o *Obj) MarshalJSON() ([]byte, error) { return AppendJSON(nil, o), nil }
func (o *Obj) UnmarshalJSON(b []byte) error {
	v, e := Parse(b)
	if e != nil {
		return e
	}
	obj := asObj(v)
	if obj == nil {
		return fmt.Errorf("checkpoint requires object")
	}
	*o = *obj
	return nil
}

type coordinate struct {
	Value   int64
	Present bool
}

func checkpointCoordinate(v optInt) coordinate { return coordinate{v.v, v.ok} }
func (v coordinate) restore() optInt           { return optInt{v.Value, v.Present} }

type blockState struct {
	Index                     int
	Type                      string
	Closed                    bool
	Text, Arguments, ID, Name string
}
type streamState struct {
	Turn, Step                           coordinate
	NextIndex, ReasoningIndex, TextIndex int
	ToolIndexes                          map[string]int
	Blocks                               []blockState
	SourceSeqs                           []int64
	Records                              json.RawMessage
	Usage                                json.RawMessage
	UsageSeen                            bool
}
type projectorState struct {
	Input                      Input
	GoToDSH, DSHToGo           map[int64]int64
	ToolResults                map[int64]*Obj
	GoalMessages               map[string]json.RawMessage
	GoalAdmissions             map[string]int64
	Turn, Step                 coordinate
	Provider, Model, HeaderSig string
	HasHeaderSig               bool
	Stream                     *streamState
	Count                      int64
}

func (p *Projector) checkpoint() ([]byte, error) {
	c := projectorState{Input: p.input, GoToDSH: p.goToDsh, DSHToGo: p.dshToGo, ToolResults: p.toolResults, GoalMessages: map[string]json.RawMessage{}, GoalAdmissions: p.goalAdmissions, Turn: checkpointCoordinate(p.turn), Step: checkpointCoordinate(p.step), Provider: p.provider, Model: p.model, HeaderSig: p.headerSig, HasHeaderSig: p.hasHeaderSig, Count: p.count}
	for k, v := range p.goalMessages {
		c.GoalMessages[k] = AppendJSON(nil, v)
	}
	if t := p.stream; t != nil {
		c.Stream = &streamState{Turn: checkpointCoordinate(t.turn), Step: checkpointCoordinate(t.step), NextIndex: t.nextIndex, ReasoningIndex: t.reasoningIndex, TextIndex: t.textIndex, ToolIndexes: t.toolIndexes, SourceSeqs: t.sourceSeqs, Records: AppendJSON(nil, t.records), Usage: AppendJSON(nil, t.usage), UsageSeen: t.usageSeen}
		for _, b := range t.blocks {
			c.Stream.Blocks = append(c.Stream.Blocks, blockState{b.index, b.typ, b.closed, b.text, b.arguments, b.id, b.name})
		}
	}
	return json.Marshal(c)
}
func restoreProjector(b []byte) (*Projector, error) {
	var c projectorState
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.GoToDSH == nil || c.DSHToGo == nil || c.ToolResults == nil || c.GoalAdmissions == nil || c.Count < 0 {
		return nil, fmt.Errorf("invalid projection checkpoint")
	}
	p := NewProjector(c.Input)
	p.goToDsh, p.dshToGo, p.toolResults, p.goalAdmissions = c.GoToDSH, c.DSHToGo, c.ToolResults, c.GoalAdmissions
	p.turn, p.step = c.Turn.restore(), c.Step.restore()
	p.provider, p.model, p.headerSig, p.hasHeaderSig, p.count = c.Provider, c.Model, c.HeaderSig, c.HasHeaderSig, c.Count
	for k, v := range c.GoalMessages {
		x, err := Parse(v)
		if err != nil {
			return nil, err
		}
		p.goalMessages[k] = x
	}
	if t := c.Stream; t != nil {
		s := &stream{turn: t.Turn.restore(), step: t.Step.restore(), nextIndex: t.NextIndex, reasoningIndex: t.ReasoningIndex, textIndex: t.TextIndex, toolIndexes: t.ToolIndexes, sourceSeqs: t.SourceSeqs, usageSeen: t.UsageSeen}
		for _, b := range t.Blocks {
			s.blocks = append(s.blocks, &streamBlock{b.Index, b.Type, b.Closed, b.Text, b.Arguments, b.ID, b.Name})
		}
		r, err := Parse(t.Records)
		if err != nil {
			return nil, err
		}
		s.records, _ = r.([]any)
		s.usage, err = Parse(t.Usage)
		if err != nil {
			return nil, err
		}
		p.stream = s
	}
	return p, nil
}

// StreamSnapshot is presentation state only; execution stays with the Agent.
func (h *History) StreamSnapshot() json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.projector == nil {
		return h.streamSnapshot
	}
	return h.projector.StreamSnapshot()
}
func (p *Projector) StreamSnapshot() json.RawMessage {
	if p == nil || p.stream == nil {
		return json.RawMessage("null")
	}
	s := p.stream
	o := NewObj().Set("id", "point-stream:"+p.input.ID+":"+s.turn.String()+":"+s.step.String()).Set("records", s.records)
	s.turn.set(o, "turn")
	s.step.set(o, "step")
	return AppendJSON(nil, o)
}
func (h *History) ProjectionCheckpoint() ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.projector == nil {
		if err := h.restoreCheckpoint(); err != nil {
			return nil, err
		}
	}
	return h.projector.checkpoint()
}
func ProjectorFromCheckpoint(b []byte) (*Projector, error) { return restoreProjector(b) }

func (p *Projector) StreamPosition() (string, int) {
	if p == nil || p.stream == nil {
		return "", 0
	}
	s := p.stream
	return "point-stream:" + p.input.ID + ":" + s.turn.String() + ":" + s.step.String(), len(s.records)
}

// StreamDelta encodes only new records; serializing the whole in-flight answer
// per native token would make the streaming path quadratic again.
func (p *Projector) StreamDelta(previous string, offset int) (json.RawMessage, string, int) {
	id, count := p.StreamPosition()
	if id == "" {
		return json.RawMessage("null"), "", 0
	}
	if id != previous || offset > count || offset < 0 {
		offset = 0
	}
	s := p.stream
	out := NewObj().Set("id", id).Set("offset", float64(offset)).Set("records", s.records[offset:])
	s.turn.set(out, "turn")
	s.step.set(out, "step")
	return AppendJSON(nil, out), id, count
}
