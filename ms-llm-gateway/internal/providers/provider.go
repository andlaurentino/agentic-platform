// Package providers holds the upstream providers from llms.yaml: their model
// lists (configured or discovered) and the protocol adapters that serve an
// OpenAI Chat Completions request from an upstream.
package providers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/v2/option"

	"agentic-platform/ms-llm-gateway/internal/llmconfig"
	"agentic-platform/ms-llm-gateway/internal/wire"
)

const discoveryTimeout = 10 * time.Second

// Discovery reports how a provider's model list was obtained.
type Discovery struct {
	Status      string    `json:"status"` // off | ok | failed
	Error       string    `json:"error,omitempty"`
	Count       int       `json:"count"`
	RefreshedAt time.Time `json:"refreshedAt,omitzero"`
}

// Provider is one upstream from llms.yaml.
type Provider struct {
	Cfg       llmconfig.Provider
	http      *http.Client
	anthropic anthropic.Client

	mu        sync.RWMutex
	models    []llmconfig.Model
	discovery Discovery
}

// NewProvider builds a provider with its configured models. Call Discover to
// list models from OpenAI-compatible upstreams that configure discover.
func NewProvider(cfg llmconfig.Provider) *Provider {
	p := &Provider{
		Cfg:       cfg,
		http:      &http.Client{},
		models:    slices.Clone(cfg.Models),
		discovery: Discovery{Status: "off", Count: len(cfg.Models)},
	}
	if cfg.Type == llmconfig.TypeAnthropic {
		var opts []anthropicopt.RequestOption
		if cfg.BaseURL != "" {
			opts = append(opts, anthropicopt.WithBaseURL(cfg.BaseURL))
		}
		p.anthropic = anthropic.NewClient(opts...)
	}
	return p
}

// Models returns the provider's current models.
func (p *Provider) Models() []llmconfig.Model {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return slices.Clone(p.models)
}

// Model finds a current model by upstream id.
func (p *Provider) Model(id string) (llmconfig.Model, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	i := slices.IndexFunc(p.models, func(m llmconfig.Model) bool { return m.ID == id })
	if i < 0 {
		return llmconfig.Model{}, false
	}
	return p.models[i], true
}

func (p *Provider) Discovery() Discovery {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.discovery
}

// Discover lists the upstream's models (only for providers that configure
// discover). On failure the previous list is kept.
func (p *Provider) Discover(ctx context.Context) {
	if p.Cfg.Discover == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	ids, err := p.listModels(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	if err != nil {
		if p.discovery.Status != "failed" {
			slog.Warn("model discovery failed; keeping the previous model list", "provider", p.Cfg.Key, "err", err)
		}
		p.discovery = Discovery{Status: "failed", Error: err.Error(), Count: len(p.models), RefreshedAt: now}
		return
	}
	p.models = mergeModels(p.Cfg.Models, filterModels(ids, *p.Cfg.Discover))
	p.discovery = Discovery{Status: "ok", Count: len(p.models), RefreshedAt: now}
}

// Call is one resolved chat completion request.
type Call struct {
	ClientModel string // the id the client asked for, e.g. "claude-haiku/claude-haiku-5-5"
	Model       llmconfig.Model
	Req         *wire.ChatRequest
	Raw         []byte // the original body, forwarded to OpenAI-compatible upstreams
}

// Output is where a successful response goes. Errors are returned instead, so
// the server can write them as JSON, or as a stream error once streaming started.
type Output struct {
	W   http.ResponseWriter
	SSE *wire.SSE
}

// Result is what the server logs about a call.
type Result struct {
	Usage wire.Usage
}

// Chat serves one chat completion from the upstream.
func (p *Provider) Chat(ctx context.Context, c Call, out Output) (Result, *wire.Error) {
	if p.Cfg.NeedsKey() && p.Cfg.APIKey() == "" {
		return Result{}, wire.Errorf(http.StatusBadGateway, "upstream_auth_error", "missing_api_key",
			"%s: %s is not set on the LLM gateway", p.Cfg.Name, p.Cfg.APIKeyEnv)
	}
	switch p.Cfg.Type {
	case llmconfig.TypeAnthropic:
		return p.anthropicChat(ctx, c, out)
	default:
		return p.openaiChat(ctx, c, out)
	}
}

// upstreamMessage extracts error.message from an upstream JSON error body.
func upstreamMessage(raw []byte) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		if body.Error.Message != "" {
			return body.Error.Message
		}
		if body.Message != "" {
			return body.Message
		}
	}
	if len(raw) > 300 {
		raw = raw[:300]
	}
	return string(raw)
}

// upstreamError maps an upstream HTTP status to the gateway's response.
func (p *Provider) upstreamError(status int, msg, modelID string) *wire.Error {
	name := p.Cfg.Name
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		if p.Cfg.NeedsKey() {
			return wire.Errorf(http.StatusBadGateway, "upstream_auth_error", "invalid_api_key", "%s: %s is missing or invalid (%s)", name, p.Cfg.APIKeyEnv, msg)
		}
		return wire.Errorf(http.StatusBadGateway, "upstream_auth_error", "", "%s rejected the gateway's credentials: %s", name, msg)
	case status == http.StatusNotFound:
		return wire.Errorf(http.StatusNotFound, "invalid_request_error", "model_not_found", "%s: model %q is not available: %s", name, modelID, msg)
	case status == http.StatusTooManyRequests:
		return wire.Errorf(http.StatusTooManyRequests, "rate_limit_error", "rate_limit_exceeded", "%s: rate limited, try again shortly: %s", name, msg)
	case status >= 400 && status < 500:
		return wire.Errorf(status, "invalid_request_error", "", "%s rejected the request: %s", name, msg)
	}
	return wire.Errorf(http.StatusBadGateway, "upstream_error", "", "%s failed (%d): %s", name, status, msg)
}
