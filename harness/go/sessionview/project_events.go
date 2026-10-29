package sessionview

import (
	"errors"
	"fmt"
	"strconv"
)

func roundKey(v *Obj) string {
	return Stringify([]any{v.Get("goalId"), v.Get("revision"), v.Get("round")})
}

// project is projectGoSessionEventsBase for the embedded profile.
func (p *Projector) project(src native) ([]*Obj, error) {
	typ := src.typ
	dshSeq := p.count
	// The event was parsed for this call and is not read again by anything that
	// mutates it, so it is projected in place rather than copied.
	var data any = src.data
	var prefix []*Obj

	if typ == "agent/inbox/spliced" {
		d := asObj(data)
		if d == nil {
			return nil, errors.New("agent/inbox/spliced carries no data")
		}
		if !d.Has("inserted") || d.Get("inserted") == nil {
			d.Set("inserted", []any{})
		}
		if !d.Has("removedCount") || d.Get("removedCount") == nil {
			d.Set("removedCount", float64(0))
		}
		if inserted, ok := d.Get("inserted").([]any); ok {
			for _, m := range inserted {
				if msg := asObj(m); msg != nil {
					if source := asObj(msg.Get("source")); source != nil && source.Get("kind") == "goal" {
						p.goalMessages[roundKey(source)] = Clone(msg)
					}
				}
			}
		}
	}
	if typ == "goal/round" {
		d := asObj(data)
		key := roundKey(d)
		message, ok := p.goalMessages[key]
		if !ok {
			source := NewObj().Set("kind", "goal")
			for _, k := range d.Keys() {
				source.Set(k, Clone(d.Get(k)))
			}
			message = NewObj().Set("role", "user").Set("source", source).
				Set("content", []any{NewObj().Set("type", "text").Set("text", "Continue the admitted goal round.")})
		}
		delete(p.goalMessages, key)
		p.goalAdmissions[key] = dshSeq
		typ = "user/message"
		normalized, err := p.normalizeMessage(message, "user", src.seq)
		if err != nil {
			return nil, err
		}
		data = normalized
	} else if typ == "user/message" {
		if d := asObj(data); d != nil {
			if source := asObj(d.Get("source")); source != nil && source.Get("kind") == "goal" {
				if seq, ok := p.goalAdmissions[roundKey(source)]; ok {
					p.goToDsh[src.seq] = seq
					return nil, nil
				}
			}
		}
	}

	switch typ {
	case "compaction/summary", "compaction/prune":
		shadowed, err := p.shadowPrice(asObj(data))
		if err != nil {
			return nil, err
		}
		data = shadowed
	case "image/offload":
		d := asObj(data)
		targets, ok := d.Get("targets").([]any)
		if !ok || len(targets) == 0 {
			return nil, errors.New("Invalid native image-offload targets")
		}
		out := Clone(d).(*Obj)
		mapped := make([]any, len(targets))
		for i, t := range targets {
			target := asObj(t)
			n, ok := asInt(target.Get("seq"))
			seq, found := p.goToDsh[n]
			if !ok || !found {
				return nil, errors.New("Image offload has no durable projected target")
			}
			item := Clone(target).(*Obj)
			item.Set("seq", float64(seq))
			mapped[i] = item
		}
		out.Set("targets", mapped)
		data = out
	case "turn/start":
		d := normalizeCoordinates(data, false)
		data = d
		p.turn, p.step = optOf(d.Get("turn")), some(0)
	case "step/start":
		d := normalizeCoordinates(data, true)
		data = d
		p.turn, p.step = optOf(d.Get("turn")), optOf(d.Get("step"))
		p.stream = newStream(p.turn, p.step)
	case "step/end":
		data = normalizeCoordinates(data, true)
		p.step = some(0)
	case "turn/end":
		data = normalizeTurnEnd(normalizeCoordinates(data, false))
		p.turn, p.step = some(0), some(0)
	case "user/message":
		message, err := p.normalizeMessage(data, "user", src.seq)
		if err != nil {
			return nil, err
		}
		data = message
	case "system/message":
		d := normalizeCoordinates(data, true)
		message, err := p.normalizeMessage(asObj(data).Get("message"), "system", src.seq)
		if err != nil {
			return nil, err
		}
		d.Set("message", message)
		data = d
	case "developer/message":
		orig := asObj(data)
		var headerSeq int64
		hasHeader := false
		if orig.Has("headerSeq") {
			n, ok := asInt(orig.Get("headerSeq"))
			mapped, found := p.goToDsh[n]
			if !ok || !found {
				return nil, errors.New("Developer update has no durable projected header")
			}
			headerSeq, hasHeader = mapped, true
		}
		d := normalizeCoordinates(data, true)
		message, err := p.normalizeMessage(orig.Get("message"), "developer", src.seq)
		if err != nil {
			return nil, err
		}
		d.Set("message", message)
		if hasHeader {
			d.Set("headerSeq", float64(headerSeq))
		}
		data = d
	case "assistant/attempt":
		if p.stream == nil {
			return nil, errors.New("assistant/attempt without an open stream")
		}
		orig := asObj(data)
		d := normalizeCoordinates(data, true)
		if st := orig.Get("stream"); st != nil {
			d.Set("stream", Clone(st))
		} else {
			d.Set("stream", append([]any(nil), p.stream.records...))
		}
		for _, seq := range p.stream.sourceSeqs {
			p.goToDsh[seq] = dshSeq
		}
		p.stream = newStream(optOf(d.Get("turn")), optOf(d.Get("step")))
		data = d
	case "assistant/message":
		d := asObj(data)
		if d == nil {
			return nil, errors.New("assistant/message carries no data")
		}
		return p.projectAssistantMessage(src, d, dshSeq)
	case "tool/call":
		d := Clone(data)
		obj := asObj(d)
		if obj == nil {
			return nil, errors.New("tool/call carries no data")
		}
		if _, ok := obj.Get("arguments").(string); !ok {
			args := obj.Get("arguments")
			if args == nil {
				args = NewObj()
			}
			obj.Set("arguments", Stringify(args))
		}
		data = obj
	case "tool/result":
		d, err := p.toolResult(src, data)
		if err != nil {
			return nil, err
		}
		data = d
	case "request/header":
		d, extra, err := p.requestHeader(src, asObj(data), dshSeq)
		if err != nil {
			return nil, err
		}
		prefix = extra.prefix
		if extra.typ != "" {
			typ = extra.typ
		}
		data = d
	case "assistant/chunk":
		d := asObj(data)
		if d == nil {
			return nil, errors.New("assistant/chunk carries no data")
		}
		return p.projectChunk(src, d)
	}

	projected := p.event(src, typ, data, dshSeq+int64(len(prefix)))
	if src.typ == "goal/round" {
		projected.Set("surfaceOp", "append")
	}
	if src.hasSurfaceOp {
		op, err := p.projectedSurfaceOp(src)
		if err != nil {
			return nil, err
		}
		projected.Set("surfaceOp", op)
	}
	if src.hasSourceSeqs {
		mapped := make([]any, len(src.sourceEventSeqs))
		for i, s := range src.sourceEventSeqs {
			if v, ok := p.goToDsh[s]; ok {
				mapped[i] = float64(v)
			}
		}
		projected.Set("sourceEventSeqs", mapped)
	}
	return append(prefix, projected), nil
}

func (p *Projector) shadowPrice(data *Obj) (*Obj, error) {
	rangeObj := asObj(data.Get("shadowedRange"))
	seqs, ok := data.Get("shadowedSeqs").([]any)
	if rangeObj == nil || !ok || len(seqs) == 0 || !sameNumber(seqs[0], rangeObj.Get("start")) || !sameNumber(seqs[len(seqs)-1], rangeObj.Get("end")) {
		return nil, errors.New("Invalid native compaction shadow-price range")
	}
	project := func(v any) (float64, error) {
		n, ok := asInt(v)
		mapped, found := p.goToDsh[n]
		if !ok || !found {
			return 0, errors.New("Compaction shadow price has no durable projected endpoint")
		}
		return float64(mapped), nil
	}
	start, err := project(rangeObj.Get("start"))
	if err != nil {
		return nil, err
	}
	end, err := project(rangeObj.Get("end"))
	if err != nil {
		return nil, err
	}
	out := Clone(data).(*Obj)
	out.Set("shadowedRange", NewObj().Set("start", start).Set("end", end))
	mapped := make([]any, len(seqs))
	for i, s := range seqs {
		if mapped[i], err = project(s); err != nil {
			return nil, err
		}
	}
	out.Set("shadowedSeqs", mapped)
	return out, nil
}

func sameNumber(a, b any) bool {
	x, okA := a.(float64)
	y, okB := b.(float64)
	return okA && okB && x == y
}

func (p *Projector) toolResult(src native, data any) (any, error) {
	block := asObj(data)
	if block == nil {
		block = NewObj()
	}
	callID := block.Get("toolCallId")
	var original *Obj
	if src.hasSurfaceOp {
		if op := asObj(src.surfaceOp); op != nil && op.Get("op") == "replace" {
			if start, ok := asInt(op.Get("start")); ok {
				original = p.toolResults[start]
			}
		}
	}
	var result *Obj
	if original != nil {
		originalResult := asObj(original.Get("message"))
		originalMeta := original.Get("meta")
		replacementMeta := block.Get("meta")
		if replacementMeta == nil {
			replacementMeta = nil
		}
		if originalResult == nil || originalResult.Get("role") != "tool" ||
			Stringify(originalResult.Get("toolCallId")) != Stringify(callID) ||
			(originalResult.Get("isError") == true) != (block.Get("isError") == true) ||
			metaKey(originalMeta, original.Has("meta")) != metaKey(replacementMeta, block.Has("meta") && block.Get("meta") != nil) {
			return nil, errors.New("Go tool/result replacement may change only content")
		}
		result = Clone(original).(*Obj)
		content, err := normalizeBlocks(block.Get("content"))
		if err != nil {
			return nil, err
		}
		asObj(result.Get("message")).Set("content", content)
	} else {
		content, err := normalizeBlocks(block.Get("content"))
		if err != nil {
			return nil, err
		}
		message := NewObj().
			Set("id", pointMessageID(p.input, src.seq, "tool-result")).
			Set("role", "tool").
			Set("source", toolSource(callID))
		if callID != nil {
			message.Set("toolCallId", Clone(callID))
		}
		if block.Get("isError") == true {
			message.Set("isError", true)
		}
		message.Set("content", content)
		result = NewObj()
		p.turn.set(result, "turn")
		p.step.set(result, "step")
		result.Set("message", message)
		if meta := block.Get("meta"); meta != nil {
			result.Set("meta", Clone(meta))
		}
	}
	// A later replacement reuses the envelope and swaps the content, so the
	// content itself is not retained.
	lean := Clone(result).(*Obj)
	if message := asObj(lean.Get("message")); message != nil {
		message.Set("content", []any{})
	}
	p.toolResults[src.seq] = lean
	return result, nil
}

func toolSource(callID any) *Obj {
	s := NewObj().Set("kind", "tool")
	if callID != nil {
		s.Set("callId", Clone(callID))
	}
	return s
}

// metaKey is JSON.stringify(meta) with undefined represented by "".
func metaKey(meta any, present bool) string {
	if !present {
		return "\x00undefined"
	}
	return Stringify(meta)
}

type headerExtra struct {
	prefix []*Obj
	typ    string
}

func (p *Projector) requestHeader(src native, data *Obj, dshSeq int64) (any, headerExtra, error) {
	var extra headerExtra
	if data == nil {
		return nil, extra, errors.New("request/header carries no data")
	}
	previous := p.stream
	nextStream := newStream(
		pick(optOf(data.Get("turn")), p.turn),
		pick(optOf(data.Get("step")), p.step),
	)
	header := p.normalizeHeader(data)
	signature := Stringify(header)
	recorded := ""
	if r, ok := data.Get("reason").(string); ok {
		switch r {
		case "initial", "resume", "change", "series":
			recorded = r
		}
	}
	configProvider, _ := asObj(header.Get("config")).Get("provider").(string)
	configModel, _ := asObj(header.Get("config")).Get("model").(string)
	p.provider, p.model = configProvider, configModel
	startsSeries := data.Get("startsSeries") == true
	var out any
	switch {
	case recorded != "":
		d := NewObj().Set("header", header).Set("reason", recorded)
		if recorded != "series" && startsSeries {
			d.Set("startsSeries", true)
		}
		out = d
	case !p.hasHeaderSig:
		out = NewObj().Set("header", header).Set("reason", "initial")
	case p.headerSig != signature:
		d := NewObj().Set("header", header).Set("reason", "change")
		if startsSeries {
			d.Set("startsSeries", true)
		}
		out = d
	case startsSeries:
		out = NewObj().Set("header", header).Set("reason", "series")
	default:
		if previous != nil && len(previous.records) > 0 {
			attemptData := NewObj()
			previous.turn.set(attemptData, "turn")
			previous.step.set(attemptData, "step")
			attemptData.Set("stream", append([]any(nil), previous.records...))
			attempt := p.event(src, "assistant/attempt", attemptData, dshSeq)
			extra.prefix = append(extra.prefix, attempt)
			for _, seq := range previous.sourceSeqs {
				p.goToDsh[seq] = dshSeq
			}
		}
		extra.typ = "point/request-attempt"
		out = Clone(data)
	}
	p.stream = nextStream
	p.headerSig, p.hasHeaderSig = signature, true
	return out, extra, nil
}

func pick(a, b optInt) optInt {
	if a.ok {
		return a
	}
	return b
}

// CheckCurrent is currentSessionEvent: a replacement must carry exactly its
// projected endpoints.
func CheckCurrent(event *Obj) error {
	op := asObj(event.Get("surfaceOp"))
	if op == nil || op.Get("op") != "replace" {
		return nil
	}
	if _, ok := asInt(op.Get("startSeq")); !ok {
		return errors.New("Replacement has no durable projected endpoints")
	}
	if _, ok := asInt(op.Get("endSeq")); !ok {
		return errors.New("Replacement has no durable projected endpoints")
	}
	if op.Len() != 3 {
		return errors.New("Invalid current replacement shape")
	}
	return nil
}

var _ = fmt.Sprint
var _ = strconv.Itoa
