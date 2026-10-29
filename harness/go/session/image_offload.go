package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

const imageOffloadEventType = "image/offload"
const maxSafeInteger = uint64(9007199254740991)

type imageOffloadTarget struct {
	Seq          uint64 `json:"seq"`
	ImageIndexes []int  `json:"imageIndexes"`
}

// OffloadOldestImages records one image/offload decision selecting the oldest
// retained input occurrences in seqs. Assistant output is skipped. Indexes
// count every image, including ones already omitted. It reports whether any
// occurrence was recorded.
func (s *Session) OffloadOldestImages(seqs []uint64, count int) (bool, error) {
	if s == nil {
		return false, errors.New("image/offload: session is required")
	}
	if count < 0 {
		return false, errors.New("image/offload: count must not be negative")
	}
	events := s.Events()
	surface := s.SurfaceSeqs()
	messages, err := projectedSurfaceMessages(events, surface)
	if err != nil {
		return false, err
	}
	bySeq := make(map[uint64]Event, len(events))
	for _, event := range events {
		bySeq[event.Seq] = event
	}
	targets := make([]imageOffloadTarget, 0)
	for _, seq := range seqs {
		if count == 0 {
			break
		}
		event, ok := bySeq[seq]
		if !ok {
			return false, fmt.Errorf("image/offload: missing event seq %d", seq)
		}
		if event.Type != "user/message" && event.Type != "tool/result" {
			continue
		}
		indexes := make([]int, 0)
		walkImages(messages[seq].Content, func(index int, block ContentBlock) bool {
			if count == 0 {
				return false
			}
			if !block.Offloaded {
				indexes = append(indexes, index)
				count--
			}
			return true
		})
		if len(indexes) > 0 {
			targets = append(targets, imageOffloadTarget{Seq: seq, ImageIndexes: indexes})
		}
	}
	if len(targets) == 0 {
		return false, nil
	}
	if _, err := s.Append(imageOffloadEventType, struct {
		Targets []imageOffloadTarget `json:"targets"`
	}{Targets: targets}); err != nil {
		return false, err
	}
	return true, nil
}

// ProjectedSurfaceMessages returns the current derived messages for seqs, with
// durable image omissions applied. Seqs that are not current surface nodes
// still decode when the event exists, without an offload mark.
func (s *Session) ProjectedSurfaceMessages(seqs []uint64) ([]Message, error) {
	if s == nil {
		return nil, errors.New("image/offload: session is required")
	}
	events := s.Events()
	marked, err := projectedSurfaceMessages(events, s.SurfaceSeqs())
	if err != nil {
		return nil, err
	}
	bySeq := make(map[uint64]Event, len(events))
	for _, event := range events {
		bySeq[event.Seq] = event
	}
	messages := make([]Message, 0, len(seqs))
	for _, seq := range seqs {
		if message, ok := marked[seq]; ok {
			messages = append(messages, cloneMessage(message))
			continue
		}
		event, ok := bySeq[seq]
		if !ok {
			return nil, fmt.Errorf("image/offload: missing event seq %d", seq)
		}
		message, err := decodeSurfaceMessage(event)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// ImageOffloadFollowsLatestHeader reports whether an image omission was
// recorded after the latest request header. That changes the model-visible
// input without moving surface identities, so the next request starts a series.
func (s *Session) ImageOffloadFollowsLatestHeader() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var header, offload uint64
	for _, event := range s.events {
		switch event.Type {
		case "request/header":
			header = event.Seq
		case imageOffloadEventType:
			offload = event.Seq
		}
	}
	return offload > header
}

func projectedSurfaceMessages(events []Event, surface []uint64) (map[uint64]Message, error) {
	return projectedSurfaceMessagesWithDecoder(events, surface, decodeSurfaceMessage)
}

// The decoder is shared by retained-surface image projections.
func projectedSurfaceMessagesWithDecoder(events []Event, surface []uint64, decode func(Event) (Message, error)) (map[uint64]Message, error) {
	messages := make(map[uint64]Message)
	types := make(map[uint64]string, len(events))
	current := make([]uint64, 0)
	for _, event := range events {
		types[event.Seq] = event.Type
		if isSurfaceEventType(event.Type) {
			message, err := decode(event)
			if err != nil {
				return nil, err
			}
			messages[event.Seq] = message
			current, err = replaySurface(current, event)
			if err != nil {
				return nil, err
			}
			continue
		}
		if event.Type != imageOffloadEventType {
			continue
		}
		targets, err := decodeImageOffload(event.Data)
		if err != nil {
			return nil, err
		}
		nodes := make(map[uint64]struct{}, len(current))
		for _, seq := range current {
			nodes[seq] = struct{}{}
		}
		seen := make(map[uint64]struct{}, len(targets))
		for _, target := range targets {
			if _, duplicate := seen[target.Seq]; duplicate {
				return nil, fmt.Errorf("image/offload: duplicate target seq %d", target.Seq)
			}
			seen[target.Seq] = struct{}{}
			if _, ok := nodes[target.Seq]; !ok {
				return nil, fmt.Errorf("image/offload: target seq %d is not a current surface node", target.Seq)
			}
			source := messages[target.Seq]
			if types[target.Seq] != "user/message" && types[target.Seq] != "tool/result" {
				return nil, fmt.Errorf("image/offload: target seq %d must be user/message or tool/result", target.Seq)
			}
			marked, err := markMessageImages(source, target.ImageIndexes)
			if err != nil {
				return nil, err
			}
			messages[target.Seq] = marked
		}
	}
	if len(current) != len(surface) {
		return nil, errors.New("image/offload: surface does not match the event log")
	}
	result := make(map[uint64]Message, len(surface))
	for index, seq := range surface {
		if current[index] != seq {
			return nil, errors.New("image/offload: surface does not match the event log")
		}
		message, ok := messages[seq]
		if !ok {
			return nil, fmt.Errorf("harness: context references missing seq %d", seq)
		}
		result[seq] = message
	}
	return result, nil
}

func replaySurface(surface []uint64, event Event) ([]uint64, error) {
	if len(event.SurfaceOp) == 0 || string(event.SurfaceOp) == `"append"` {
		return append(surface, event.Seq), nil
	}
	var operation struct {
		Op    string `json:"op"`
		Start uint64 `json:"start"`
		End   uint64 `json:"end"`
	}
	if err := json.Unmarshal(event.SurfaceOp, &operation); err != nil || operation.Op != "replace" {
		return nil, fmt.Errorf("harness: invalid surface operation at seq %d", event.Seq)
	}
	startIndex, endIndex := -1, -1
	for index, seq := range surface {
		if seq == operation.Start && startIndex < 0 {
			startIndex = index
		}
		if seq == operation.End {
			endIndex = index
		}
	}
	if startIndex < 0 || endIndex < startIndex {
		return nil, fmt.Errorf("harness: replacement at seq %d names a missing or reversed surface range %d-%d", event.Seq, operation.Start, operation.End)
	}
	next := make([]uint64, 0, len(surface)-(endIndex-startIndex)+1)
	next = append(next, surface[:startIndex]...)
	next = append(next, event.Seq)
	next = append(next, surface[endIndex+1:]...)
	return next, nil
}

func eventsHaveImageOffload(events []Event) bool {
	for _, event := range events {
		if event.Type == imageOffloadEventType {
			return true
		}
	}
	return false
}

func decodeImageOffload(data json.RawMessage) ([]imageOffloadTarget, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || len(object) != 1 {
		return nil, errors.New("image/offload: data must contain a nonempty targets array")
	}
	rawTargets, ok := object["targets"]
	if !ok {
		return nil, errors.New("image/offload: data must contain a nonempty targets array")
	}
	var targetsRaw []json.RawMessage
	if err := json.Unmarshal(rawTargets, &targetsRaw); err != nil || len(targetsRaw) == 0 {
		return nil, errors.New("image/offload: data must contain a nonempty targets array")
	}
	targets := make([]imageOffloadTarget, 0, len(targetsRaw))
	for _, rawTarget := range targetsRaw {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawTarget, &fields); err != nil || len(fields) != 2 {
			return nil, errors.New("image/offload: each target must contain a seq and nonempty imageIndexes")
		}
		rawSeq, hasSeq := fields["seq"]
		rawIndexes, hasIndexes := fields["imageIndexes"]
		seq, seqOK := canonicalIndex(rawSeq)
		if !hasSeq || !hasIndexes || !seqOK {
			return nil, errors.New("image/offload: each target must contain a seq and nonempty imageIndexes")
		}
		var indexesRaw []json.RawMessage
		if err := json.Unmarshal(rawIndexes, &indexesRaw); err != nil || len(indexesRaw) == 0 {
			return nil, errors.New("image/offload: each target must contain a seq and nonempty imageIndexes")
		}
		indexes := make([]int, len(indexesRaw))
		previous := -1
		for index, raw := range indexesRaw {
			value, ok := canonicalIndex(raw)
			if !ok || value > uint64(int(^uint(0)>>1)) || int(value) <= previous {
				return nil, errors.New("image/offload: imageIndexes must be strictly increasing non-negative safe integers")
			}
			indexes[index] = int(value)
			previous = indexes[index]
		}
		targets = append(targets, imageOffloadTarget{Seq: seq, ImageIndexes: indexes})
	}
	return targets, nil
}

func canonicalIndex(raw json.RawMessage) (uint64, bool) {
	if len(raw) == 0 || raw[0] == '-' {
		return 0, false
	}
	literal := string(raw)
	if literal == "" || (len(literal) > 1 && literal[0] == '0') {
		return 0, false
	}
	for _, character := range literal {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(literal, 10, 64)
	if err != nil || value > maxSafeInteger {
		return 0, false
	}
	return value, true
}

func markMessageImages(message Message, indexes []int) (Message, error) {
	imageIndex := 0
	selected := 0
	content, err := markImageBlocks(message.Content, indexes, &imageIndex, &selected)
	if err != nil {
		return Message{}, err
	}
	if selected != len(indexes) {
		return Message{}, fmt.Errorf("image/offload: image index %d does not exist", indexes[selected])
	}
	message.Content = content
	return message, nil
}

func markImageBlocks(blocks []ContentBlock, indexes []int, imageIndex, selected *int) ([]ContentBlock, error) {
	next := make([]ContentBlock, len(blocks))
	for index, block := range blocks {
		if block.Type == harnessmodel.BlockImage {
			if *selected < len(indexes) && *imageIndex == indexes[*selected] {
				if block.Offloaded {
					return nil, fmt.Errorf("image/offload: image index %d is already offloaded", *imageIndex)
				}
				block.Offloaded = true
				*selected++
			}
			*imageIndex++
		} else if block.Type == harnessmodel.BlockToolResult {
			content, err := markImageBlocks(block.Content, indexes, imageIndex, selected)
			if err != nil {
				return nil, err
			}
			block.Content = content
		}
		next[index] = block
	}
	return next, nil
}

func walkImages(blocks []ContentBlock, visit func(index int, block ContentBlock) bool) {
	imageIndex := 0
	var walk func([]ContentBlock) bool
	walk = func(blocks []ContentBlock) bool {
		for _, block := range blocks {
			if block.Type == harnessmodel.BlockImage {
				if !visit(imageIndex, block) {
					return false
				}
				imageIndex++
				continue
			}
			if block.Type == harnessmodel.BlockToolResult && !walk(block.Content) {
				return false
			}
		}
		return true
	}
	walk(blocks)
}
