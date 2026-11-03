// Package claudetest is a scripted fake of the Anthropic Messages API for tests.
// Point the SDK at it with option.WithBaseURL(srv.URL) or ANTHROPIC_BASE_URL.
package claudetest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

// Block is one content block of a scripted reply.
type Block map[string]any

func Text(s string) Block { return Block{"type": "text", "text": s} }

func Thinking(text, signature string) Block {
	return Block{"type": "thinking", "thinking": text, "signature": signature}
}

func ToolUse(id, name string, input map[string]any) Block {
	return Block{"type": "tool_use", "id": id, "name": name, "input": input}
}

// Turn is one scripted reply.
type Turn struct {
	Blocks     []Block
	StopReason string // defaults to "tool_use" if any block is a tool_use, else "end_turn"
}

// Server replays turns in order and records every request body.
type Server struct {
	*httptest.Server
	mu       sync.Mutex
	turns    []Turn
	requests []map[string]any
}

func New(turns ...Turn) *Server {
	s := &Server{turns: turns}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Requests returns the decoded request bodies received so far.
func (s *Server) Requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.requests...)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)

	s.mu.Lock()
	s.requests = append(s.requests, req)
	if len(s.turns) == 0 {
		s.mu.Unlock()
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"claudetest: no scripted turns left"}}`, http.StatusBadRequest)
		return
	}
	turn := s.turns[0]
	s.turns = s.turns[1:]
	s.mu.Unlock()

	stop := turn.StopReason
	if stop == "" {
		stop = "end_turn"
		for _, b := range turn.Blocks {
			if b["type"] == "tool_use" {
				stop = "tool_use"
			}
		}
	}
	model, _ := req["model"].(string)

	if stream, _ := req["stream"].(bool); !stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_test", "type": "message", "role": "assistant", "model": model,
			"content": turn.Blocks, "stop_reason": stop,
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 5},
		})
		return
	}
	WriteStream(w, model, turn.Blocks, stop)
}

// WriteStream writes blocks as an Anthropic SSE stream. Text is split into two
// deltas so callers can see partial events.
func WriteStream(w http.ResponseWriter, model string, blocks []Block, stopReason string) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	send := func(event string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	send("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_test", "type": "message", "role": "assistant", "model": model, "content": []any{},
		"stop_reason": nil, "usage": map[string]any{"input_tokens": 10, "output_tokens": 0},
	}})
	for i, b := range blocks {
		switch b["type"] {
		case "text":
			text, _ := b["text"].(string)
			send("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "text", "text": ""}})
			half := len(text) / 2
			for _, part := range []string{text[:half], text[half:]} {
				if part != "" {
					send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": part}})
				}
			}
		case "thinking":
			send("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "thinking_delta", "thinking": b["thinking"]}})
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "signature_delta", "signature": b["signature"]}})
		case "tool_use":
			input, _ := json.Marshal(b["input"])
			send("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": b["id"], "name": b["name"], "input": map[string]any{}}})
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
		}
		send("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason}, "usage": map[string]any{"output_tokens": 5}})
	send("message_stop", map[string]any{"type": "message_stop"})
}
