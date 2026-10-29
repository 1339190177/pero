// Package sessionview projects a native Point Session log into the event
// vocabulary the DSH conversation renders, and serves it as windows.
//
// It was ported from Point go/harness/plugins/session-projection.mjs for the
// embedded profile. The original JavaScript oracle and conformance tests are
// not included in this extraction; behavioral equivalence is not established
// here. The historical (non-embedded) profile is not ported.
package sessionview

import (
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/phodal/pero/harness/go/session"
)

// Input identifies the Session being projected.
type Input struct{ ID, Provider, Model string }

// ErrIncompatibleJSON mirrors DSH_SESSION_JSON_INCOMPATIBLE.
var ErrIncompatibleJSON = errors.New("DSH_SESSION_JSON_INCOMPATIBLE: native Session JSON cannot enter DSH without changing its value")

// optInt is a JavaScript number that may be undefined (a Turn before any
// turn/start, a coordinate the log did not carry).
type optInt struct {
	v  int64
	ok bool
}

func some(v int64) optInt { return optInt{v: v, ok: true} }

func (o optInt) String() string {
	if !o.ok {
		return "undefined"
	}
	return strconv.FormatInt(o.v, 10)
}

func (o optInt) set(obj *Obj, key string) {
	if o.ok {
		obj.Set(key, float64(o.v))
	} else {
		obj.Delete(key)
	}
}

type streamBlock struct {
	index     int
	typ       string
	closed    bool
	text      string
	arguments string
	id        string
	name      string
}

type stream struct {
	turn, step     optInt
	nextIndex      int
	reasoningIndex int // -1 = undefined
	textIndex      int
	toolIndexes    map[string]int
	blocks         []*streamBlock
	sourceSeqs     []int64
	records        []any
	usage          any
	usageSeen      bool
}

func newStream(turn, step optInt) *stream {
	return &stream{turn: turn, step: step, reasoningIndex: -1, textIndex: -1, toolIndexes: map[string]int{}}
}

// native is one decoded native log event.
type native struct {
	seq             int64
	time            time.Time
	typ             string
	data            any
	surfaceOp       any
	hasSurfaceOp    bool
	sourceEventSeqs []int64
	hasSourceSeqs   bool
}

// Projector turns native events into projected events, one at a time, keeping
// the cross-event state the vocabulary needs (stream assembly, native-to-
// projected sequence maps, replaceable tool results, goal admissions).
type Projector struct {
	input          Input
	goToDsh        map[int64]int64
	dshToGo        map[int64]int64
	toolResults    map[int64]*Obj
	goalMessages   map[string]any
	goalAdmissions map[string]int64
	turn, step     optInt
	provider       string
	model          string
	headerSig      string
	hasHeaderSig   bool
	stream         *stream
	count          int64 // projected events produced so far (the next projected seq)
}

func NewProjector(input Input) *Projector {
	return &Projector{
		input: input, goToDsh: map[int64]int64{}, dshToGo: map[int64]int64{}, toolResults: map[int64]*Obj{},
		goalMessages: map[string]any{}, goalAdmissions: map[string]int64{},
		turn: some(0), step: some(0), provider: input.Provider, model: input.Model,
	}
}

// Count is the number of projected events so far; the next event gets this seq.
func (p *Projector) Count() int64 { return p.count }

// NativeSeq maps a projected seq back to the native event that produced it.
func (p *Projector) NativeSeq(projected int64) (int64, bool) {
	v, ok := p.dshToGo[projected]
	return v, ok
}

// Decode converts a stored event for projection. It rejects JSON a JavaScript
// consumer would silently change (a negative zero).
func Decode(e session.Event) (native, error) {
	n := native{seq: int64(e.Seq), time: e.Time, typ: e.Type}
	var err error
	if len(e.Data) > 0 {
		if n.data, err = Parse(e.Data); err != nil {
			return native{}, wrapIncompatible(err)
		}
	}
	if len(e.SurfaceOp) > 0 {
		n.hasSurfaceOp = true
		if n.surfaceOp, err = Parse(e.SurfaceOp); err != nil {
			return native{}, wrapIncompatible(err)
		}
	}
	if len(e.SourceEventSeqs) > 0 {
		n.hasSourceSeqs = true
		for _, s := range e.SourceEventSeqs {
			n.sourceEventSeqs = append(n.sourceEventSeqs, int64(s))
		}
	}
	return n, nil
}

func wrapIncompatible(err error) error {
	if errors.Is(err, ErrUnrepresentableNumber) {
		return ErrIncompatibleJSON
	}
	return err
}

// Project projects one native event and returns the projected events, which
// have already been recorded in the sequence maps.
func (p *Projector) Project(e session.Event) ([]*Obj, error) {
	src, err := Decode(e)
	if err != nil {
		return nil, err
	}
	return p.projectNative(src)
}

func (p *Projector) projectNative(src native) ([]*Obj, error) {
	out, err := p.project(src)
	if err != nil {
		return nil, err
	}
	for _, ev := range out {
		seq, _ := asInt(ev.Get("seq"))
		p.dshToGo[seq] = src.seq
	}
	if len(out) > 0 {
		seq, _ := asInt(out[len(out)-1].Get("seq"))
		p.goToDsh[src.seq] = seq
	}
	p.count += int64(len(out))
	return out, nil
}

// ---- helpers mirroring JavaScript semantics ----

func asObj(v any) *Obj { o, _ := v.(*Obj); return o }

func asString(v any) (string, bool) { s, ok := v.(string); return s, ok }

func nonEmptyString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok && s != ""
}

const maxSafe = 1<<53 - 1

// asInt is Number.isSafeInteger.
func asInt(v any) (int64, bool) {
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || math.Abs(f) > maxSafe {
		return 0, false
	}
	return int64(f), true
}

func optOf(v any) optInt {
	if n, ok := asInt(v); ok {
		return some(n)
	}
	return optInt{}
}

func projectionTime(t time.Time) float64 {
	if t.IsZero() {
		return float64(time.Now().UnixMilli())
	}
	return float64(t.UnixMilli())
}

func pointMessageID(input Input, seq int64, suffix string) string {
	return "point:" + input.ID + ":" + strconv.FormatInt(seq, 10) + ":" + suffix
}

func truthyString(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}

// ---- content blocks (content-codec.mjs: contentBlockToDsh) ----

func normalizeBlocks(blocks any) ([]any, error) {
	arr, ok := blocks.([]any)
	if !ok {
		return []any{}, nil
	}
	out := make([]any, len(arr))
	for i, b := range arr {
		switch t := b.(type) {
		case *Obj:
			c, err := blockToDSH(t)
			if err != nil {
				return nil, err
			}
			out[i] = c
		case []any:
			return nil, errors.New("INVALID_CONTENT_BLOCK: invalid content")
		default:
			out[i] = b
		}
	}
	return out, nil
}

func blockToDSH(block *Obj) (*Obj, error) {
	typ, _ := asString(block.Get("type"))
	if typ == "tool-call" {
		out := NewObj()
		for _, k := range block.Keys() {
			if k == "toolCallId" || k == "id" || k == "arguments" {
				continue
			}
			out.Set(k, Clone(block.Get(k)))
		}
		id, hasID := any(nil), false
		if pid, ok := nonEmptyString(block.Get("id")); ok {
			id, hasID = pid, true
		} else if block.Has("toolCallId") {
			id, hasID = block.Get("toolCallId"), true
		}
		if hasID && id != nil {
			out.Set("id", Clone(id))
		} else if hasID {
			out.Set("id", nil)
		}
		if block.Has("arguments") {
			args := block.Get("arguments")
			if s, ok := args.(string); ok {
				out.Set("arguments", s)
			} else {
				out.Set("arguments", Stringify(args))
			}
		}
		return out, nil
	}
	if typ == "tool-result" {
		if content, ok := block.Get("content").([]any); ok {
			out := Clone(block).(*Obj)
			mapped := make([]any, len(content))
			for i, item := range content {
				switch t := item.(type) {
				case *Obj:
					c, err := blockToDSH(t)
					if err != nil {
						return nil, err
					}
					mapped[i] = c
				case []any:
					return nil, errors.New("INVALID_CONTENT_BLOCK: invalid content")
				default:
					return nil, errors.New("INVALID_CONTENT_BLOCK: invalid content")
				}
			}
			out.Set("content", mapped)
			return out, nil
		}
	}
	return Clone(block).(*Obj), nil
}

// ---- message / coordinate / header normalization ----

func (p *Projector) normalizeMessage(message any, role string, seq int64) (*Obj, error) {
	msg := asObj(message)
	var src *Obj
	if s := asObj(msg.Get("source")); s != nil {
		if _, ok := nonEmptyString(s.Get("kind")); ok {
			src = Clone(s).(*Obj)
		}
	}
	if src == nil {
		switch role {
		case "assistant":
			src = NewObj().Set("kind", "model").
				Set("provider", truthyString(p.provider, p.input.Provider, "point")).
				Set("model", truthyString(p.model, p.input.Model, "unknown"))
		case "system":
			src = NewObj().Set("kind", "system-prompt")
		case "developer":
			src = NewObj().Set("kind", "tool-registry")
		default:
			src = NewObj().Set("kind", "user")
		}
	}
	if role == "assistant" {
		src.Set("kind", "model")
		if _, ok := nonEmptyString(src.Get("provider")); !ok {
			src.Set("provider", truthyString(p.provider, p.input.Provider, "point"))
		}
		if _, ok := nonEmptyString(src.Get("model")); !ok {
			src.Set("model", truthyString(p.model, p.input.Model, "unknown"))
		}
	}
	id, ok := nonEmptyString(msg.Get("id"))
	if !ok {
		id = pointMessageID(p.input, seq, "message")
	}
	content, err := normalizeBlocks(msg.Get("content"))
	if err != nil {
		return nil, err
	}
	return NewObj().Set("id", id).Set("role", role).Set("content", content).Set("source", src), nil
}

func normalizeCoordinates(data any, includeStep bool) *Obj {
	source := asObj(data)
	out := NewObj()
	if source == nil {
		return out
	}
	for _, k := range source.Keys() {
		if k == "Turn" || k == "Step" {
			continue
		}
		out.Set(k, Clone(source.Get(k)))
	}
	turn, step := source.Get("turn"), source.Get("step")
	if _, ok := asInt(turn); !ok {
		turn = source.Get("Turn")
	}
	if _, ok := asInt(step); !ok {
		step = source.Get("Step")
	}
	if n, ok := asInt(turn); ok {
		out.Set("turn", float64(n))
	}
	if n, ok := asInt(step); ok && includeStep {
		out.Set("step", float64(n))
	}
	return out
}

func normalizeTurnEnd(data *Obj) *Obj {
	if r := data.Get("reason"); r != nil {
		if _, isObj := r.(*Obj); isObj {
			return Clone(data).(*Obj)
		}
		if _, isArr := r.([]any); isArr {
			return Clone(data).(*Obj)
		}
	}
	with := func(reason *Obj) *Obj {
		out := Clone(data).(*Obj)
		out.Set("reason", reason)
		return out
	}
	switch reason, _ := data.Get("reason").(string); reason {
	case "max-tokens":
		return with(NewObj().Set("kind", "max-tokens"))
	case "aborted":
		return with(NewObj().Set("kind", "aborted").Set("reason", NewObj().Set("kind", "legacy")))
	case "error":
		message := "Go Agent turn failed"
		if s, ok := data.Get("error").(string); ok {
			message = s
		}
		return with(NewObj().Set("kind", "error").Set("error", NewObj().Set("message", message).Set("code", "UNKNOWN")))
	case "blocked":
		return with(NewObj().Set("kind", "blocked"))
	}
	return with(NewObj().Set("kind", "completed"))
}

func (p *Projector) normalizeHeader(data *Obj) *Obj {
	config := NewObj()
	config.Set("provider", truthyString(str(data.Get("provider")), p.provider, p.input.Provider, "point-adapter"))
	config.Set("model", truthyString(str(data.Get("model")), p.model, p.input.Model, "default"))
	if s, ok := nonEmptyString(data.Get("reasoningEffort")); ok {
		config.Set("reasoningEffort", s)
	}
	if f, ok := data.Get("temperature").(float64); ok {
		config.Set("temperature", f)
	}
	if n, ok := asInt(data.Get("maxTokens")); ok && n > 0 {
		config.Set("maxTokens", float64(n))
	}
	if stop, ok := data.Get("stop").([]any); ok && len(stop) > 0 {
		config.Set("stop", Clone(stop))
	}
	header := NewObj().Set("config", config)
	if defaults := asObj(data.Get("adapterDefaults")); defaults != nil {
		re, mt := defaults.Get("reasoningEffort") == true, defaults.Get("maxTokens") == true
		if re || mt {
			d := NewObj()
			if re {
				d.Set("reasoningEffort", true)
			}
			if mt {
				d.Set("maxTokens", true)
			}
			header.Set("adapterDefaults", d)
		}
	}
	if tools, ok := data.Get("tools").([]any); ok && len(tools) > 0 {
		out := make([]any, len(tools))
		for i, t := range tools {
			tool := asObj(t)
			item := NewObj()
			if tool.Has("name") {
				item.Set("name", Clone(tool.Get("name")))
			}
			description := ""
			if s, ok := tool.Get("description").(string); ok {
				description = s
			}
			item.Set("description", description)
			params := tool.Get("parameters")
			if params == nil {
				params = tool.Get("inputSchema")
			}
			if params == nil {
				params = NewObj()
			}
			item.Set("parameters", Clone(params))
			if tool.Get("deferLoading") == true {
				item.Set("deferLoading", true)
			}
			out[i] = item
		}
		header.Set("tools", out)
	}
	return header
}

func str(v any) string { s, _ := v.(string); return s }

func normalizeFinish(reason any) *Obj {
	s, _ := reason.(string)
	if s == "aborted" || s == "error" {
		return NewObj().Set("kind", s).Set("failure", NewObj().Set("message", "Go provider finished with "+s).Set("code", "UNKNOWN"))
	}
	if s != "" {
		return NewObj().Set("kind", s)
	}
	return NewObj().Set("kind", "stop")
}

func (p *Projector) event(src native, typ string, data any, seq int64) *Obj {
	return NewObj().Set("type", typ).Set("seq", float64(seq)).Set("time", projectionTime(src.time)).Set("data", data)
}

// ---- stream assembly ----

func (p *Projector) ensureStream(turn, step optInt) *stream {
	if p.stream == nil || p.stream.turn != turn || p.stream.step != step {
		p.stream = newStream(turn, step)
	}
	return p.stream
}

func pointToolCallID(s *stream) string {
	return "point-call-" + s.turn.String() + "-" + s.step.String() + "-" + strconv.Itoa(s.nextIndex)
}

func (s *stream) start(blockType, key string, block *Obj) *streamBlock {
	entry := &streamBlock{index: s.nextIndex, typ: blockType}
	s.nextIndex++
	if block != nil {
		if id, ok := nonEmptyString(block.Get("id")); ok {
			entry.id = id
		}
		if name, ok := nonEmptyString(block.Get("name")); ok {
			entry.name = name
		}
	}
	s.blocks = append(s.blocks, entry)
	if blockType == "reasoning" && s.reasoningIndex < 0 {
		s.reasoningIndex = entry.index
	}
	if blockType == "text" && s.textIndex < 0 {
		s.textIndex = entry.index
	}
	if blockType == "tool-call" {
		s.toolIndexes[key] = entry.index
	}
	return entry
}

func (s *stream) block(index int) *streamBlock {
	if index < 0 {
		return nil
	}
	for _, b := range s.blocks {
		if b.index == index {
			return b
		}
	}
	return nil
}

func (s *stream) blockForDelta(block *Obj, position int) *streamBlock {
	switch t, _ := block.Get("type").(string); t {
	case "reasoning":
		if e := s.block(s.reasoningIndex); e != nil {
			return e
		}
		return s.start("reasoning", "reasoning", nil)
	case "text":
		if e := s.block(s.textIndex); e != nil {
			return e
		}
		return s.start("text", "text", nil)
	case "tool-call":
		raw, hasRaw := nonEmptyString(block.Get("id"))
		if !hasRaw {
			raw, hasRaw = nonEmptyString(block.Get("toolCallId"))
		}
		var key string
		if hasRaw {
			key = "id:" + raw
		} else {
			key = "position:" + strconv.Itoa(position) + ":" + jsToString(block.Get("name"))
		}
		if idx, ok := s.toolIndexes[key]; ok {
			if e := s.block(idx); e != nil {
				return e
			}
		}
		id := raw
		if !hasRaw {
			id = pointToolCallID(s)
		}
		nb := NewObj().Set("id", id)
		if name := block.Get("name"); name != nil {
			nb.Set("name", name)
		}
		return s.start("tool-call", key, nb)
	}
	return nil
}

// jsToString is `${value ?? ”}` for the value kinds a block name can hold.
func jsToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return string(appendNumber(nil, t))
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	return Stringify(v)
}

func (s *stream) matchFinal(block *Obj, occurrence int) *streamBlock {
	typ, _ := block.Get("type").(string)
	switch typ {
	case "reasoning", "text":
		n := 0
		for _, c := range s.blocks {
			if c.typ == typ {
				if n == occurrence {
					if c.closed {
						return nil
					}
					return c
				}
				n++
			}
		}
		return nil
	case "tool-call":
		id, hasID := nonEmptyString(block.Get("id"))
		if hasID {
			for _, e := range s.blocks {
				if e.typ == "tool-call" && !e.closed && e.id == id {
					return e
				}
			}
		}
		for _, e := range s.blocks {
			if e.typ == "tool-call" && !e.closed {
				return e
			}
		}
		return nil
	}
	for _, e := range s.blocks {
		if e.typ == typ && !e.closed {
			return e
		}
	}
	return nil
}

func (s *stream) push(t time.Time, chunk *Obj) {
	s.records = append(s.records, NewObj().Set("type", "chunk").Set("time", projectionTime(t)).Set("chunk", chunk))
}

func (p *Projector) projectChunk(src native, data *Obj) ([]*Obj, error) {
	turn, step := optOf(data.Get("turn")), optOf(data.Get("step"))
	if !turn.ok {
		turn = p.turn
	}
	if !step.ok {
		step = p.step
	}
	s := p.ensureStream(turn, step)
	chunkObj := asObj(data.Get("chunk"))
	content, _ := chunkObj.Get("content").([]any)
	for position, raw := range content {
		var rawObj *Obj
		switch t := raw.(type) {
		case *Obj:
			rawObj = t
		case []any:
			return nil, errors.New("INVALID_CONTENT_BLOCK: invalid content")
		default:
			continue
		}
		block, err := blockToDSH(rawObj)
		if err != nil {
			return nil, err
		}
		previous := s.nextIndex
		entry := s.blockForDelta(block, position)
		if entry == nil {
			continue
		}
		if s.nextIndex != previous {
			s.push(src.time, NewObj().Set("type", "block-start").Set("index", float64(entry.index)).Set("blockType", entry.typ))
		}
		switch btype, _ := block.Get("type").(string); btype {
		case "text", "reasoning":
			text, _ := block.Get("text").(string)
			entry.text += text
			kind := "text-delta"
			if btype == "reasoning" {
				kind = "reasoning-delta"
			}
			s.push(src.time, NewObj().Set("type", kind).Set("index", float64(entry.index)).Set("text", text))
		case "tool-call":
			delta, _ := rawObj.Get("argumentsDelta").(string)
			if name, ok := nonEmptyString(block.Get("name")); ok {
				entry.name = name
			}
			entry.arguments += delta
			c := NewObj().Set("type", "tool-call-delta").Set("index", float64(entry.index))
			if entry.id != "" {
				c.Set("id", entry.id)
			}
			if name, ok := nonEmptyString(block.Get("name")); ok {
				c.Set("name", name)
			}
			c.Set("argumentsDelta", delta)
			s.push(src.time, c)
		}
	}
	if usage := chunkObj.Get("usage"); usage != nil && usage != false {
		s.usage = Clone(usage)
	}
	s.sourceSeqs = append(s.sourceSeqs, src.seq)
	return nil, nil
}

func (p *Projector) projectedSurfaceOp(src native) (any, error) {
	if !src.hasSurfaceOp {
		return "append", nil
	}
	if s, ok := src.surfaceOp.(string); ok && s == "append" {
		return "append", nil
	}
	op := asObj(src.surfaceOp)
	if op == nil || op.Get("op") != "replace" {
		return nil, errors.New("Invalid native replacement surface metadata")
	}
	start, okS := asInt(op.Get("start"))
	end, okE := asInt(op.Get("end"))
	if !okS || !okE {
		return nil, errors.New("Invalid native replacement surface metadata")
	}
	ds, okDS := p.goToDsh[start]
	de, okDE := p.goToDsh[end]
	if !okDS || !okDE {
		return nil, errors.New("Replacement has no durable projected endpoints")
	}
	return NewObj().Set("op", "replace").Set("startSeq", float64(ds)).Set("endSeq", float64(de)), nil
}

func (p *Projector) projectAssistantMessage(src native, data *Obj, firstSeq int64) ([]*Obj, error) {
	turn, step := optOf(data.Get("turn")), optOf(data.Get("step"))
	if !turn.ok {
		turn = p.turn
	}
	if !step.ok {
		step = p.step
	}
	s := p.ensureStream(turn, step)
	message, err := p.normalizeMessage(data.Get("message"), "assistant", src.seq)
	if err != nil {
		return nil, err
	}
	var events []*Obj
	record := func(chunk *Obj) { s.push(src.time, chunk) }
	build := func(base *Obj) (*Obj, error) {
		d := NewObj()
		for _, k := range base.Keys() {
			d.Set(k, Clone(base.Get(k)))
		}
		turn.set(d, "turn")
		step.set(d, "step")
		d.Set("message", message)
		d.Set("stream", append([]any(nil), s.records...))
		projected := p.event(src, "assistant/message", d, firstSeq+int64(len(events)))
		op, err := p.projectedSurfaceOp(src)
		if err != nil {
			return nil, err
		}
		projected.Set("surfaceOp", op)
		for _, seq := range s.sourceSeqs {
			p.goToDsh[seq] = firstSeq + int64(len(events))
		}
		return projected, nil
	}
	if data.Get("interrupted") == true {
		if s.usage != nil && !s.usageSeen {
			record(NewObj().Set("type", "usage").Set("usage", Clone(s.usage)))
			s.usageSeen = true
		}
		projected, err := build(data)
		if err != nil {
			return nil, err
		}
		return append(events, projected), nil
	}
	occurrences := map[string]int{}
	content, _ := message.Get("content").([]any)
	for _, item := range content {
		block := asObj(item)
		if block == nil {
			return nil, errors.New("assistant message content block is not an object")
		}
		btype, _ := block.Get("type").(string)
		occurrence := occurrences[btype]
		occurrences[btype] = occurrence + 1
		entry := s.matchFinal(block, occurrence)
		if entry == nil {
			var key string
			if btype == "tool-call" {
				key = "id:" + jsToString(block.Get("id"))
			} else {
				key = "final:" + btype + ":" + strconv.Itoa(occurrence)
			}
			entry = s.start(btype, key, block)
			record(NewObj().Set("type", "block-start").Set("index", float64(entry.index)).Set("blockType", btype))
			switch btype {
			case "text", "reasoning":
				entry.text = ""
				if t, ok := block.Get("text").(string); ok {
					entry.text = t
				} else if block.Get("text") != nil {
					entry.text = jsToString(block.Get("text"))
				}
				kind := "text-delta"
				if btype == "reasoning" {
					kind = "reasoning-delta"
				}
				record(NewObj().Set("type", kind).Set("index", float64(entry.index)).Set("text", entry.text))
			case "tool-call":
				if id, ok := nonEmptyString(block.Get("id")); ok {
					entry.id = id
				} else if entry.id == "" {
					entry.id = pointToolCallID(s)
				}
				entry.name = ""
				if n := block.Get("name"); n != nil {
					entry.name = jsToString(n)
				}
				entry.arguments = ""
				if a := block.Get("arguments"); a != nil {
					entry.arguments = jsToString(a)
				}
				c := NewObj().Set("type", "tool-call-delta").Set("index", float64(entry.index)).Set("id", entry.id)
				if len(entry.name) > 0 {
					c.Set("name", entry.name)
				}
				c.Set("argumentsDelta", entry.arguments)
				record(c)
			}
		}
		if btype == "tool-call" {
			if _, ok := nonEmptyString(block.Get("id")); !ok {
				id := entry.id
				if id == "" {
					id = pointToolCallID(s)
				}
				block.Set("id", id)
			}
			if _, ok := block.Get("name").(string); !ok {
				block.Set("name", entry.name)
			}
			if _, ok := block.Get("arguments").(string); !ok {
				args := block.Get("arguments")
				if args == nil {
					args = NewObj()
				}
				block.Set("arguments", Stringify(args))
			}
		}
		if !entry.closed {
			record(NewObj().Set("type", "block-end").Set("index", float64(entry.index)).Set("block", Clone(block)))
			entry.closed = true
		}
	}
	usage := s.usage
	if usage == nil {
		usage = data.Get("usage")
	}
	if usage != nil && usage != false && !s.usageSeen {
		record(NewObj().Set("type", "usage").Set("usage", Clone(usage)))
		s.usageSeen = true
	}
	record(NewObj().Set("type", "finish").Set("reason", normalizeFinish(data.Get("finish"))))
	rest := NewObj()
	for _, k := range data.Keys() {
		if k != "finish" {
			rest.Set(k, data.Get(k))
		}
	}
	projected, err := build(rest)
	if err != nil {
		return nil, err
	}
	return append(events, projected), nil
}
