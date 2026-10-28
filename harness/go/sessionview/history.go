package sessionview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/phodal/pero/harness/go/internal/jsonwire"
	"github.com/phodal/pero/harness/go/session"
)

// DefaultPageBytes bounds a window by weight as well as by message count: request
// headers and embedded token streams make some turns hundreds of kilobytes.
const DefaultPageBytes = 1_000_000

const minPageMessages = 6

// Request selects a window of the projected log, as sessionHistoryPage does:
// MaxMessages (default 50) counts append-origin user and assistant messages; a
// turn window (MinMessages and MinTurns together) stops at a turn start; MaxBytes
// weighs the page. Zero values mean absent.
type Request struct {
	BeforeSeq   *int64
	ThroughSeq  *int64
	MaxMessages int
	MinMessages int
	MinTurns    int
	MaxBytes    int
}

// Page is one contiguous window of encoded projected events.
type Page struct {
	Records  [][]byte // each is one projected event, already JSON
	FirstSeq int64
	HasMore  bool
}

// Canvas is a published canvas receipt found in the log.
type Canvas struct {
	Digest      string `json:"digest"`
	Revision    string `json:"revision"`
	Title       string `json:"title"`
	Sequence    int64  `json:"sequence"`
	PublishedAt string `json:"publishedAt"`
}

// Digest is what replaying a whole log leaves behind besides its events: the
// facts a conversation reads from history without showing every message.
type PendingApproval struct {
	NativeSeq uint64
	Request   json.RawMessage
}

type Digest struct {
	Canvases      []Canvas
	ModelSelected []byte // data of the last session/model-selected
	AgentPreset   []byte // data of the last agent-preset/selected
	Approvals     []PendingApproval
	LastUser      *LastUser
}

// LastUser is the newest user message, enough to label the Session.
type LastUser struct {
	Seq     int64
	Time    string
	Preview string
}

type meta struct {
	seq       int64
	typ       string
	appendOp  bool
	minSource int64 // smallest of seq and its source seqs: where a page must begin to keep its source group
	size      int
	offset    int64
	bytes     int
	native    int64
	checksum  uint32
}

// History is the projected, encoded view of one native log. It only grows: new
// native events are appended, never revised.
type History struct {
	mu                  sync.Mutex
	input               Input
	projector           *Projector
	streamSnapshot      json.RawMessage
	metas               []meta
	encoded             [][]byte
	weight              int64
	last                uint64 // last native seq projected
	canvases            map[string]Canvas
	approvals           map[string]PendingApproval
	digest              Digest
	diskDir, checkpoint string
	segments            []diskSegment
	persisted           int
}

func NewHistory(input Input) *History {
	return &History{input: input, projector: NewProjector(input), canvases: map[string]Canvas{}, approvals: map[string]PendingApproval{}}
}

// LastNativeSeq is the native seq up to which the log has been projected.
func (h *History) LastNativeSeq() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

// Weight is the retained size in bytes, for cache accounting.
func (h *History) Weight() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.weight
}

// Len is the number of projected events.
func (h *History) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.metas)
}

// ErrDiverged reports a log that no longer extends what was projected.
var ErrDiverged = errors.New("sessionview: the native log does not extend the projected history")

// Append projects native events, which must continue the log contiguously.
// Events already projected are skipped, so a caller may pass an overlapping range.
func (h *History) Append(events []session.Event) error {
	return h.AppendContext(context.Background(), events)
}
func (h *History) AppendContext(ctx context.Context, events []session.Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.projector == nil {
		if err := h.restoreCheckpoint(); err != nil {
			return err
		}
	}
	for index, e := range events {
		if index%256 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if e.Seq <= h.last {
			continue
		}
		if e.Seq != h.last+1 {
			return fmt.Errorf("%w: expected seq %d, got %d", ErrDiverged, h.last+1, e.Seq)
		}
		if !jsonwire.NumbersAreSafe(e.Data) || !jsonwire.NumbersAreSafe(e.SurfaceOp) {
			return ErrIncompatibleJSON
		}
		n, err := Decode(e)
		if err != nil {
			return err
		}
		out, err := h.projector.projectNative(n)
		if err != nil {
			return fmt.Errorf("sessionview: project seq %d (%s): %w", e.Seq, e.Type, err)
		}
		for _, ev := range out {
			if err := CheckCurrent(ev); err != nil {
				return err
			}
			raw := AppendJSON(nil, ev)
			m := meta{size: utf16Len(raw), bytes: len(raw), native: int64(e.Seq)}
			m.seq, _ = asInt(ev.Get("seq"))
			m.typ, _ = ev.Get("type").(string)
			m.appendOp = ev.Get("surfaceOp") == "append"
			m.minSource = m.seq
			if seqs, ok := ev.Get("sourceEventSeqs").([]any); ok {
				for _, s := range seqs {
					v := int64(0) // JavaScript's Math.min coerces a missing source to 0
					if f, ok := s.(float64); ok {
						v = int64(f)
					}
					if v < m.minSource {
						m.minSource = v
					}
				}
			}
			h.metas = append(h.metas, m)
			h.encoded = append(h.encoded, raw)
			h.weight += int64(len(raw)) + 64
		}
		h.observe(n)
		h.last = e.Seq
	}
	return nil
}

var (
	digestPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	revisionPattern = regexp.MustCompile(`^(sha256:)?[a-f0-9]{64}$`)
)

// observe folds the facts a conversation reads from the whole log.
func (h *History) observe(n native) {
	switch n.typ {
	case "approval/asked", "approval/decided":
		id, _ := asObj(n.data).Get("id").(string)
		if n.typ == "approval/asked" {
			h.approvals[id] = PendingApproval{uint64(n.seq), AppendJSON(nil, n.data)}
		} else {
			delete(h.approvals, id)
		}
	case "tool/result":
		d := asObj(n.data)
		if d == nil || truthy(d.Get("isError")) {
			return
		}
		meta := asObj(d.Get("meta"))
		if meta == nil || meta.Get("kind") != "homology.canvas.render.v1" || n.seq < 1 {
			return
		}
		digest, _ := meta.Get("artifactDigest").(string)
		revision, _ := meta.Get("revision").(string)
		if !digestPattern.MatchString(digest) || !revisionPattern.MatchString(revision) {
			return
		}
		if prev, ok := h.canvases[digest]; ok && prev.Sequence >= n.seq {
			return
		}
		title := "Canvas"
		if s, ok := meta.Get("title").(string); ok && strings.TrimSpace(s) != "" {
			title = strings.TrimSpace(s)
		}
		h.canvases[digest] = Canvas{Digest: digest, Revision: revision, Title: title, Sequence: n.seq, PublishedAt: n.time.UTC().Format(time.RFC3339Nano)}
	case "session/model-selected":
		h.digest.ModelSelected = AppendJSON(nil, n.data)
	case "agent-preset/selected":
		h.digest.AgentPreset = AppendJSON(nil, n.data)
	case "user/message":
		d := asObj(n.data)
		content, _ := d.Get("content").([]any)
		if content == nil {
			content, _ = asObj(d.Get("message")).Get("content").([]any)
		}
		preview := ""
		for _, b := range content {
			block := asObj(b)
			if block != nil && block.Get("type") == "text" {
				if text, ok := block.Get("text").(string); ok && strings.TrimSpace(text) != "" {
					preview = strings.TrimSpace(text)
					break
				}
			}
		}
		h.digest.LastUser = &LastUser{Seq: n.seq, Time: n.time.UTC().Format(time.RFC3339Nano), Preview: preview}
	}
}

// Digest returns the facts folded from the whole log so far.
func (h *History) Digest() Digest {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.digest
	d.Approvals = make([]PendingApproval, 0, len(h.approvals))
	for _, a := range h.approvals {
		d.Approvals = append(d.Approvals, a)
	}
	d.Canvases = make([]Canvas, 0, len(h.canvases))
	for _, c := range h.canvases {
		d.Canvases = append(d.Canvases, c)
	}
	sort.Slice(d.Canvases, func(i, j int) bool { return d.Canvases[i].Sequence > d.Canvases[j].Sequence })
	return d
}

// NativeSeq maps a projected seq to the native event that produced it.
func (h *History) NativeSeq(projected int64) (int64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if projected < 0 || projected >= int64(len(h.metas)) {
		return 0, false
	}
	return h.metas[projected].native, true
}

// Cursor is the seq of the newest projected event, or -1 for an empty log.
func (h *History) Cursor() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return int64(len(h.metas)) - 1
}

// ErrBadRequest marks an invalid window request.
var ErrBadRequest = errors.New("sessionview: invalid history request")

// Window returns the page selected by the request, with the same cut rules as
// the JavaScript sessionHistoryPage.
func (h *History) Window(r Request) (Page, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	events := int64(len(h.metas))
	through := events - 1
	if r.ThroughSeq != nil {
		through = *r.ThroughSeq
	}
	maxMessages := r.MaxMessages
	if maxMessages == 0 {
		maxMessages = 50
	}
	minMessages, minTurns, maxBytes := r.MinMessages, r.MinTurns, r.MaxBytes
	switch {
	case through < -1:
		return Page{}, fmt.Errorf("%w: throughSeq", ErrBadRequest)
	case through >= events:
		return Page{}, fmt.Errorf("%w: the cut is beyond the current log", ErrBadRequest)
	case r.BeforeSeq != nil && *r.BeforeSeq < 0:
		return Page{}, fmt.Errorf("%w: beforeSeq", ErrBadRequest)
	case maxMessages <= 0, maxBytes < 0:
		return Page{}, fmt.Errorf("%w: limits", ErrBadRequest)
	case minMessages < 0 || minTurns < 0 || (minTurns > 0 && (minMessages <= 0 || minMessages > maxMessages)):
		return Page{}, fmt.Errorf("%w: turn window", ErrBadRequest)
	}
	end := through + 1
	if r.BeforeSeq != nil && *r.BeforeSeq < end {
		end = *r.BeforeSeq
	}
	count, turns, bytes := 0, 0, 0
	var cut int64
	for index := end - 1; index >= 0; index-- {
		m := h.metas[index]
		if maxBytes > 0 {
			bytes += m.size
		}
		if minTurns > 0 && m.typ == "turn/start" {
			turns++
			if turns >= minTurns && count >= minMessages {
				cut = index
				break
			}
		}
		if (m.typ != "user/message" && m.typ != "assistant/message") || !m.appendOp {
			continue
		}
		count++
		if count >= maxMessages || maxBytes > 0 && bytes >= maxBytes && count >= minPageMessages {
			cut = m.minSource
			break
		}
	}
	if cut < 0 {
		cut = 0
	}
	page := Page{FirstSeq: cut, HasMore: cut > 0}
	if end > cut {
		var err error
		page.Records, err = h.diskRecords(cut, end)
		if err != nil {
			return Page{}, err
		}
	}
	return page, nil
}

// truthy is JavaScript truthiness for JSON values.
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t != ""
	}
	return true
}

// utf16Len is the length JavaScript reports for the same text, which is how the
// page byte budget is measured everywhere else.
func utf16Len(b []byte) int {
	n := 0
	for _, c := range b {
		switch {
		case c < 0x80:
			n++
		case c >= 0xF0:
			n += 2 // a four-byte sequence is a surrogate pair
		case c >= 0xC0:
			n++
		}
	}
	return n
}

// DefaultRequest is the window every conversation asks a Host for: at least 50
// messages across at least two turns, bounded by DefaultPageBytes.
func DefaultRequest() Request {
	return Request{MaxMessages: 50, MinMessages: 50, MinTurns: 2, MaxBytes: DefaultPageBytes}
}

// CurrentHeader is the logical Session header a conversation renders. The
// native durable header is not rewritten; this only translates its wire form.
func CurrentHeader(h session.SessionHeader, rowAgentPreset string) *Obj {
	out := NewObj().
		Set("version", float64(4)).
		Set("id", string(h.ID)).
		Set("createdAt", float64(h.CreatedAt.UnixMilli())).
		Set("isSeeded", h.SeedLength > 0)
	if h.CWD != "" {
		out.Set("cwd", h.CWD)
	}
	if h.ParentSession != "" {
		out.Set("parentSession", string(h.ParentSession))
	}
	if h.Origin != "" {
		out.Set("origin", h.Origin)
	}
	if h.DelegationDepth != 0 {
		out.Set("delegationDepth", float64(h.DelegationDepth))
	}
	if preset := truthyString(h.AgentPreset, rowAgentPreset); preset != "" {
		out.Set("agentPreset", preset)
	}
	return out
}

// RecordsJSON wraps encoded events as the {"type":"event","event":...} records
// a conversation consumes.
func RecordsJSON(events [][]byte) []byte {
	size := 2
	for _, e := range events {
		size += len(e) + 26
	}
	b := make([]byte, 0, size)
	b = append(b, '[')
	for i, e := range events {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, `{"type":"event","event":`...)
		b = append(b, e...)
		b = append(b, '}')
	}
	return append(b, ']')
}

func (h *History) PendingApprovals(firstLive uint64) []json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []json.RawMessage{}
	for _, a := range h.approvals {
		if a.NativeSeq >= firstLive {
			out = append(out, a.Request)
		}
	}
	return out
}
