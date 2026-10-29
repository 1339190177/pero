package shell

import "sync"

// tailWriter retains the source collector's bounded trailing-byte algorithm.
// No spill file or background observer can retain unbounded process output.
type tailWriter struct {
	mu        sync.Mutex
	limit     int
	data      []byte
	truncated bool
}

func newTailWriter(limit int) *tailWriter {
	return &tailWriter{limit: limit, data: make([]byte, 0, limit)}
}
func (w *tailWriter) Write(source []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(source) > w.limit {
		w.data = append(w.data[:0], source[len(source)-w.limit:]...)
		w.truncated = true
		return len(source), nil
	}
	overflow := len(w.data) + len(source) - w.limit
	if overflow > 0 {
		copy(w.data, w.data[overflow:])
		w.data = w.data[:len(w.data)-overflow]
		w.truncated = true
	}
	w.data = append(w.data, source...)
	return len(source), nil
}
func (w *tailWriter) Output() CollectedOutput {
	w.mu.Lock()
	defer w.mu.Unlock()
	return CollectedOutput{Text: string(append([]byte(nil), w.data...)), Truncated: w.truncated}
}
