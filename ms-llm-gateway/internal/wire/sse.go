package wire

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSE writes server-sent events in the Chat Completions stream format.
type SSE struct {
	w       http.ResponseWriter
	flusher http.Flusher
	started bool
}

func NewSSE(w http.ResponseWriter) *SSE {
	f, _ := w.(http.Flusher)
	return &SSE{w: w, flusher: f}
}

// Started reports whether the response headers were sent.
func (s *SSE) Started() bool { return s.started }

func (s *SSE) start() {
	if s.started {
		return
	}
	s.started = true
	h := s.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	s.w.WriteHeader(http.StatusOK)
}

// Raw writes one already-encoded data line.
func (s *SSE) Raw(data []byte) error {
	s.start()
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

// Send writes v as one event.
func (s *SSE) Send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Raw(b)
}

// Fail ends a stream that already started with an error event, which
// OpenAI clients surface as a stream error.
func (s *SSE) Fail(err *Error) {
	_ = s.Send(map[string]*Error{"error": err})
}

// Done writes the terminating [DONE] event.
func (s *SSE) Done() error { return s.Raw([]byte("[DONE]")) }
