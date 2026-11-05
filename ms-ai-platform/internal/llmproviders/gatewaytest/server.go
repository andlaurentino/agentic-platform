// Package gatewaytest is a scripted fake of an OpenAI-compatible LLM gateway
// such as ms-llm-gateway (/v1/chat/completions and /v1/models), for tests.
package gatewaytest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Model is one entry of GET /v1/models. DisplayName and Default are the
// ms-llm-gateway extensions.
type Model struct {
	ID          string
	DisplayName string
	Default     bool
}

// Models lists ids without extensions.
func Models(ids ...string) []Model {
	out := make([]Model, len(ids))
	for i, id := range ids {
		out[i] = Model{ID: id}
	}
	return out
}

// ToolCall is a scripted function call.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// Turn is one scripted reply. Error, when set, is returned instead with Status.
type Turn struct {
	Text           string
	ToolCalls      []ToolCall
	ThinkingBlocks []map[string]any // the reasoning extension (reasoning_content + thinking_blocks)
	Status         int
	Error          string
}

// Server replays turns in order and records every request.
type Server struct {
	*httptest.Server
	mu       sync.Mutex
	models   []Model
	turns    []Turn
	requests []map[string]any
	headers  []http.Header
}

// New starts a server. Use srv.URL + "/v1" as the provider baseURL.
func New(models []Model, turns ...Turn) *Server {
	s := &Server{models: models, turns: turns}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// SetModels changes what GET /v1/models returns.
func (s *Server) SetModels(models []Model) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models = models
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
		s.mu.Lock()
		data := []map[string]any{}
		for _, m := range s.models {
			entry := map[string]any{"id": m.ID, "object": "model", "created": 1, "owned_by": "test"}
			if m.DisplayName != "" {
				entry["display_name"] = m.DisplayName
			}
			if m.Default {
				entry["default"] = true
			}
			data = append(data, entry)
		}
		s.mu.Unlock()
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
		writeJSON(w, map[string]any{"error": map[string]any{"message": "gatewaytest: no scripted turns left", "type": "invalid_request_error"}})
		return
	}
	turn := s.turns[0]
	s.turns = s.turns[1:]
	s.mu.Unlock()

	if turn.Error != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(turn.Status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": turn.Error, "type": "upstream_error"}})
		return
	}

	model, _ := req["model"].(string)
	finish := "stop"
	if len(turn.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	var reasoning string
	for _, b := range turn.ThinkingBlocks {
		if t, ok := b["thinking"].(string); ok {
			reasoning += t
		}
	}

	if stream, _ := req["stream"].(bool); !stream {
		msg := map[string]any{"role": "assistant", "content": nil}
		if turn.Text != "" {
			msg["content"] = turn.Text
		}
		if len(turn.ThinkingBlocks) > 0 {
			msg["reasoning_content"] = reasoning
			msg["thinking_blocks"] = turn.ThinkingBlocks
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
	for _, b := range turn.ThinkingBlocks {
		if t, ok := b["thinking"].(string); ok && t != "" {
			send(delta(map[string]any{"reasoning_content": t}), nil)
		}
		send(delta(map[string]any{"thinking_blocks": []any{b}}), nil)
	}
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
