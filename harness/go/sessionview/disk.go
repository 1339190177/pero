package sessionview

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Each commit publishes an atomic index referencing immutable blob segments and
// a checkpoint. Appending writes only new blobs; competing processes can never
// expose a partially written checkpoint or mutate another reader's snapshot.
const diskVersion = 2
const diskMetaSize = 48

type diskSegment struct {
	Name       string
	Start, End int64
}
type diskHeader struct {
	Version    int
	Input      Input
	Last       uint64
	Digest     Digest
	Count      int
	Segments   []diskSegment
	Checkpoint string
	Extra      json.RawMessage
	Stream     json.RawMessage
}

func atomicDiskFile(path string, write func(*os.File) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		err = write(f)
	}
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err == nil {
		err = closed
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Save commits an index and drops encoded bytes after they reach disk. Extra is
// Host-owned derived metadata, tied to this exact projection checkpoint.
func (h *History) Save(path string, extra json.RawMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if h.projector == nil {
		if err := h.restoreCheckpoint(); err != nil {
			return err
		}
	}
	state, err := h.projector.checkpoint()
	if err != nil {
		return err
	}
	cp, err := os.CreateTemp(filepath.Dir(path), "checkpoint-")
	if err != nil {
		return err
	}
	cpName := cp.Name()
	defer cp.Close()
	if _, err = cp.Write(state); err != nil {
		os.Remove(cpName)
		return err
	}
	if err = cp.Sync(); err != nil {
		os.Remove(cpName)
		return err
	}
	segments := append([]diskSegment(nil), h.segments...)
	start := int64(0)
	if len(segments) > 0 {
		start = segments[len(segments)-1].End
	}
	var blobName string
	end := start
	if h.persisted < len(h.metas) {
		blob, err := os.CreateTemp(filepath.Dir(path), "blob-")
		if err != nil {
			os.Remove(cpName)
			return err
		}
		blobName = blob.Name()
		for i := h.persisted; i < len(h.metas); i++ {
			raw := h.encoded[i]
			h.metas[i].offset = end
			h.metas[i].bytes = len(raw)
			h.metas[i].checksum = crc32.ChecksumIEEE(raw)
			if _, err = blob.Write(raw); err != nil {
				break
			}
			end += int64(len(raw))
		}
		if err == nil {
			err = blob.Sync()
		}
		closed := blob.Close()
		if err == nil {
			err = closed
		}
		if err != nil {
			os.Remove(blobName)
			os.Remove(cpName)
			return err
		}
		segments = append(segments, diskSegment{filepath.Base(blobName), start, end})
	}
	snapshot := h.projector.StreamSnapshot()
	d := h.digest
	d.Approvals = make([]PendingApproval, 0, len(h.approvals))
	for _, a := range h.approvals {
		d.Approvals = append(d.Approvals, a)
	}
	d.Canvases = make([]Canvas, 0, len(h.canvases))
	for _, c := range h.canvases {
		d.Canvases = append(d.Canvases, c)
	}
	head, err := json.Marshal(diskHeader{diskVersion, h.input, h.last, d, len(h.metas), segments, filepath.Base(cpName), extra, snapshot})
	if err != nil {
		return err
	}
	err = atomicDiskFile(path, func(f *os.File) error {
		if err := binary.Write(f, binary.LittleEndian, uint64(len(head))); err != nil {
			return err
		}
		checksum := crc32.NewIEEE()
		writer := io.MultiWriter(f, checksum)
		if _, err := writer.Write(head); err != nil {
			return err
		}
		buf := make([]byte, diskMetaSize)
		for _, m := range h.metas {
			binary.LittleEndian.PutUint64(buf[0:8], uint64(m.offset))
			binary.LittleEndian.PutUint64(buf[8:16], uint64(m.bytes))
			binary.LittleEndian.PutUint64(buf[16:24], uint64(m.size))
			binary.LittleEndian.PutUint64(buf[24:32], uint64(m.minSource))
			binary.LittleEndian.PutUint64(buf[32:40], uint64(m.native))
			code := uint64(0)
			switch m.typ {
			case "turn/start":
				code = 1
			case "user/message":
				code = 2
			case "assistant/message":
				code = 3
			}
			if m.appendOp {
				code |= 8
			}
			binary.LittleEndian.PutUint64(buf[40:48], code|uint64(m.checksum)<<32)
			if _, err := writer.Write(buf); err != nil {
				return err
			}
		}
		return binary.Write(f, binary.LittleEndian, checksum.Sum32())
	})
	if err != nil {
		os.Remove(cpName)
		if blobName != "" {
			os.Remove(blobName)
		}
		return err
	}
	h.diskDir, h.checkpoint, h.segments, h.persisted = filepath.Dir(path), filepath.Base(cpName), segments, len(h.metas)
	for i := range h.encoded {
		h.encoded[i] = nil
	}
	h.streamSnapshot = snapshot
	h.weight = int64(len(h.metas)*diskMetaSize + len(snapshot))
	h.projector = nil
	// Previously opened histories may still need the old checkpoint for lazy
	// continuation. Like immutable blobs, checkpoints live until the cache
	// owner removes the cache directory; publishing an index cannot reclaim them.
	return nil
}

// OpenDisk reads the small header and fixed-width page index, never event blobs
// or projector state. The latter is decoded only for incremental continuation.
func OpenDisk(path string) (*History, json.RawMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var size uint64
	if err = binary.Read(f, binary.LittleEndian, &size); err != nil {
		return nil, nil, err
	}
	if size > 16<<20 {
		return nil, nil, fmt.Errorf("sessionview: oversized index header")
	}
	raw := make([]byte, size)
	if _, err = io.ReadFull(f, raw); err != nil {
		return nil, nil, err
	}
	var d diskHeader
	if err = json.Unmarshal(raw, &d); err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if d.Version != diskVersion || d.Count < 0 || d.Count > 5_000_000 || int64(d.Count)*diskMetaSize != info.Size()-12-int64(size) {
		return nil, nil, fmt.Errorf("sessionview: invalid index")
	}
	if filepath.Base(d.Checkpoint) != d.Checkpoint {
		return nil, nil, fmt.Errorf("sessionview: invalid checkpoint")
	}
	for _, seg := range d.Segments {
		if filepath.Base(seg.Name) != seg.Name || seg.End < seg.Start {
			return nil, nil, fmt.Errorf("sessionview: invalid segment")
		}
	}
	h := NewHistory(d.Input)
	h.projector = nil
	h.last = d.Last
	h.digest = d.Digest
	h.streamSnapshot = d.Stream
	for _, a := range d.Digest.Approvals {
		var v struct{ ID string }
		if json.Unmarshal(a.Request, &v) != nil {
			return nil, nil, fmt.Errorf("invalid approval digest")
		}
		h.approvals[v.ID] = a
	}
	for _, c := range d.Digest.Canvases {
		h.canvases[c.Digest] = c
	}
	h.diskDir, h.checkpoint, h.segments, h.persisted = filepath.Dir(path), d.Checkpoint, d.Segments, d.Count
	h.metas = make([]meta, d.Count)
	h.encoded = make([][]byte, d.Count)
	h.weight = int64(d.Count*diskMetaSize + len(d.Stream))
	buf := make([]byte, d.Count*diskMetaSize)
	if _, err = io.ReadFull(f, buf); err != nil {
		return nil, nil, err
	}
	var expected uint32
	if err = binary.Read(f, binary.LittleEndian, &expected); err != nil {
		return nil, nil, err
	}
	actual := crc32.Update(crc32.ChecksumIEEE(raw), crc32.IEEETable, buf)
	if actual != expected {
		return nil, nil, fmt.Errorf("sessionview: corrupt index checksum")
	}
	for i := range h.metas {
		b := buf[i*diskMetaSize:]
		code := binary.LittleEndian.Uint64(b[40:48])
		typ := ""
		switch code & 7 {
		case 1:
			typ = "turn/start"
		case 2:
			typ = "user/message"
		case 3:
			typ = "assistant/message"
		}
		m := meta{seq: int64(i), offset: int64(binary.LittleEndian.Uint64(b)), bytes: int(binary.LittleEndian.Uint64(b[8:])), size: int(binary.LittleEndian.Uint64(b[16:])), minSource: int64(binary.LittleEndian.Uint64(b[24:])), native: int64(binary.LittleEndian.Uint64(b[32:])), typ: typ, appendOp: code&8 != 0, checksum: uint32(code >> 32)}
		if m.offset < 0 || m.bytes < 0 || m.size < 0 || m.minSource < 0 || m.minSource > int64(i) {
			return nil, nil, fmt.Errorf("sessionview: corrupt metadata")
		}
		h.metas[i] = m
	}
	return h, d.Extra, nil
}
func (h *History) restoreCheckpoint() error {
	raw, err := os.ReadFile(filepath.Join(h.diskDir, h.checkpoint))
	if err != nil {
		return err
	}
	p, err := restoreProjector(raw)
	if err != nil {
		return err
	}
	if p.count != int64(len(h.metas)) || p.input != h.input {
		return ErrDiverged
	}
	h.projector = p
	return nil
}
func (h *History) diskRecords(start, end int64) ([][]byte, error) {
	out := make([][]byte, end-start)
	var file *os.File
	var segment diskSegment
	defer func() {
		if file != nil {
			file.Close()
		}
	}()
	for i := start; i < end; i++ {
		if h.encoded[i] != nil {
			out[i-start] = h.encoded[i]
			continue
		}
		m := h.metas[i]
		if file == nil || m.offset < segment.Start || m.offset >= segment.End {
			if file != nil {
				file.Close()
				file = nil
			}
			idx := sort.Search(len(h.segments), func(i int) bool { return h.segments[i].End > m.offset })
			if idx == len(h.segments) || h.segments[idx].Start > m.offset {
				return nil, fmt.Errorf("sessionview: missing blob")
			}
			segment = h.segments[idx]
			var err error
			file, err = os.Open(filepath.Join(h.diskDir, segment.Name))
			if err != nil {
				return nil, err
			}
		}
		if int64(m.bytes) > segment.End-m.offset {
			return nil, fmt.Errorf("sessionview: invalid blob range")
		}
		out[i-start] = make([]byte, m.bytes)
		if _, err := file.ReadAt(out[i-start], m.offset-segment.Start); err != nil {
			return nil, err
		}
		if crc32.ChecksumIEEE(out[i-start]) != m.checksum || !json.Valid(out[i-start]) {
			return nil, fmt.Errorf("sessionview: invalid blob JSON")
		}
	}
	return out, nil
}
