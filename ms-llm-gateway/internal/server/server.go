// Package server exposes the gateway's OpenAI-compatible HTTP API.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"agentic-platform/ms-llm-gateway/internal/providers"
	"agentic-platform/ms-llm-gateway/internal/wire"
)

const maxBody = 10 << 20

// Server serves /v1/models, /v1/chat/completions and /healthz.
type Server struct {
	Providers *providers.Set
	// APIKeys, when set, are the bearer tokens accepted on /v1/*.
	APIKeys []string
}

// ParseKeys splits a comma-separated key list (GATEWAY_API_KEYS).
func ParseKeys(s string) []string {
	var keys []string
	for k := range strings.SplitSeq(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/models", s.auth(http.HandlerFunc(s.models)))
	mux.Handle("POST /v1/chat/completions", s.auth(http.HandlerFunc(s.chat)))
	mux.HandleFunc("GET /healthz", s.health)
	return mux
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.APIKeys) > 0 {
			got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			ok := false
			for _, k := range s.APIKeys {
				if subtle.ConstantTimeCompare([]byte(got), []byte(k)) == 1 {
					ok = true
				}
			}
			if !ok {
				wire.WriteError(w, wire.Errorf(http.StatusUnauthorized, "invalid_request_error", "invalid_api_key", "missing or invalid gateway API key"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) models(w http.ResponseWriter, _ *http.Request) {
	wire.WriteJSON(w, wire.ModelList{Object: "list", Data: s.Providers.List()})
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			wire.WriteError(w, wire.Errorf(http.StatusRequestEntityTooLarge, "invalid_request_error", "", "request body is larger than %d bytes", maxBody))
			return
		}
		wire.WriteError(w, wire.BadRequest("read body: %v", err))
		return
	}
	var req wire.ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		wire.WriteError(w, wire.BadRequest("invalid JSON body: %v", err))
		return
	}
	if req.Model == "" {
		wire.WriteError(w, wire.BadRequest("model is required"))
		return
	}
	if len(req.Messages) == 0 {
		wire.WriteError(w, wire.BadRequest("messages must not be empty"))
		return
	}
	p, model, werr := s.Providers.Resolve(req.Model)
	if werr != nil {
		wire.WriteError(w, werr)
		return
	}

	sse := wire.NewSSE(w)
	res, werr := p.Chat(r.Context(), providers.Call{ClientModel: req.Model, Model: model, Req: &req, Raw: body}, providers.Output{W: w, SSE: sse})
	status := http.StatusOK
	if werr != nil {
		status = werr.Status
		if sse.Started() {
			sse.Fail(werr)
		} else {
			wire.WriteError(w, werr)
		}
	}
	// Never log message content: only routing, status and token counts.
	attrs := []any{
		"provider", p.Cfg.Key, "model", model.ID, "stream", req.Stream, "status", status,
		"duration_ms", time.Since(start).Milliseconds(),
		"prompt_tokens", res.Usage.PromptTokens, "completion_tokens", res.Usage.CompletionTokens,
	}
	if werr != nil {
		slog.Warn("chat completion failed", append(attrs, "error", werr.Message)...)
		return
	}
	slog.Info("chat completion", attrs...)
}

// ProviderHealth is one provider in /healthz. It never includes secrets.
type ProviderHealth struct {
	Key                   string              `json:"key"`
	Name                  string              `json:"name"`
	Type                  string              `json:"type"`
	APIKeyEnv             string              `json:"apiKeyEnv,omitempty"`
	CredentialsConfigured bool                `json:"credentialsConfigured"`
	Models                int                 `json:"models"`
	Discovery             providers.Discovery `json:"discovery"`
}

// Health reports every provider's state.
func (s *Server) Health() []ProviderHealth {
	out := []ProviderHealth{}
	for _, p := range s.Providers.Providers() {
		h := ProviderHealth{
			Key: p.Cfg.Key, Name: p.Cfg.Name, Type: string(p.Cfg.Type),
			CredentialsConfigured: p.Cfg.CredentialsConfigured(),
			Models:                len(p.Models()), Discovery: p.Discovery(),
		}
		if p.Cfg.NeedsKey() {
			h.APIKeyEnv = p.Cfg.APIKeyEnv
		}
		out = append(out, h)
	}
	return out
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	wire.WriteJSON(w, map[string]any{"status": "ok", "auth": len(s.APIKeys) > 0, "providers": s.Health()})
}
