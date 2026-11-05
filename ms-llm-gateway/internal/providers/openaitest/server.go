// Package openaitest is a scripted fake of an OpenAI-compatible endpoint
// (/v1/chat/completions and /v1/models) for tests.
package openaitest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// ToolCall is a scripted function call.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// Turn is one scripted reply.
type Turn struct {
	Text      string
	ToolCalls []ToolCall
}

// Server replays turns in order and records every request.
type Server struct {
	*httptest.Server
	Models   []string
	mu       sync.Mutex
	turns    []Turn
	requests []map[string]any
	headers  []http.Header
}

// New starts a server. Use srv.URL + "/v1" as the provider baseURL.
func New(models []string, turns ...Turn) *Server {
	s := &Server{Models: models, turns: turns}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Requests returns decoded chat completion request bodies.
func (s *Server) Requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.requests...)
}

// Headers returns the headers of each chat completion request.
func (s *Server) Headers() []http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]http.Header(nil), s.headers...)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/models"):
		data := []map[string]any{}
		for _, m := range s.Models {
			data = append(data, map[string]any{"id": m, "object": "model", "created": 1, "owned_by": "test"})
		}
		writeJSON(w, map[string]any{"object": "list", "data": data})
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		s.chat(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.headers = append(s.headers, r.Header.Clone())
	if len(s.turns) == 0 {
		s.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": map[string]any{"message": "openaitest: no scripted turns left", "type": "invalid_request_error"}})
		return
	}
	turn := s.turns[0]
	s.turns = s.turns[1:]
	s.mu.Unlock()

	model, _ := req["model"].(string)
	finish := "stop"
	if len(turn.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}

	if stream, _ := req["stream"].(bool); !stream {
		msg := map[string]any{"role": "assistant", "content": nil}
		if turn.Text != "" {
			msg["content"] = turn.Text
		}
		if len(turn.ToolCalls) > 0 {
			var calls []map[string]any
			for _, tc := range turn.ToolCalls {
				args, _ := json.Marshal(tc.Args)
				calls = append(calls, map[string]any{"id": tc.ID, "type": "function", "function": map[string]any{"name": tc.Name, "arguments": string(args)}})
			}
			msg["tool_calls"] = calls
		}
		writeJSON(w, map[string]any{
			"id": "chatcmpl-test", "object": "chat.completion", "created": 1, "model": model,
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
			"usage":   usage,
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	send := func(choices []any, extra map[string]any) {
		chunk := map[string]any{"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 1, "model": model, "choices": choices}
		for k, v := range extra {
			chunk[k] = v
		}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	delta := func(d map[string]any) []any {
		return []any{map[string]any{"index": 0, "delta": d, "finish_reason": nil}}
	}
	send(delta(map[string]any{"role": "assistant", "content": ""}), nil)
	if turn.Text != "" {
		half := len(turn.Text) / 2
		for _, part := range []string{turn.Text[:half], turn.Text[half:]} {
			if part != "" {
				send(delta(map[string]any{"content": part}), nil)
			}
		}
	}
	for i, tc := range turn.ToolCalls {
		args, _ := json.Marshal(tc.Args)
		half := len(args) / 2
		send(delta(map[string]any{"tool_calls": []any{map[string]any{"index": i, "id": tc.ID, "type": "function", "function": map[string]any{"name": tc.Name, "arguments": ""}}}}), nil)
		for _, part := range []string{string(args[:half]), string(args[half:])} {
			send(delta(map[string]any{"tool_calls": []any{map[string]any{"index": i, "function": map[string]any{"arguments": part}}}}), nil)
		}
	}
	send([]any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}, nil)
	if opts, _ := req["stream_options"].(map[string]any); opts["include_usage"] == true {
		send([]any{}, map[string]any{"usage": usage})
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
