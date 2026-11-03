// Package llms turns the providers in llms.yaml into LLM assets, and holds the
// protocol adapters (Anthropic Messages, OpenAI Chat Completions).
package llms

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/model"

	"agentic-platform/agent-assets/internal/registry"
	"agentic-platform/agent-assets/llmconfig"
	"agentic-platform/agent-assets/sdk"
)

// Settings are what builders set on an agent that uses a provider.
type Settings struct {
	Model           string `json:"model,omitempty"`
	MaxTokens       int64  `json:"maxTokens,omitempty"`
	Effort          string `json:"effort,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// DiscoveryStatus reports how the provider's model list was obtained.
type DiscoveryStatus struct {
	Status string `json:"status"` // off | ok | failed
	Error  string `json:"error,omitempty"`
	Count  int    `json:"count"`
}

const discoveryTimeout = 10 * time.Second

// Provider is the LLM asset for one llms.yaml provider.
type Provider struct {
	cfg       llmconfig.Provider
	models    []llmconfig.Model
	discovery DiscoveryStatus
}

var _ sdk.ModelProvider = (*Provider)(nil)

// NewProvider builds the asset. With discover set, OpenAI-compatible providers
// that configure `discover` list their models from GET {baseURL}/models.
func NewProvider(ctx context.Context, cfg llmconfig.Provider, discover bool) *Provider {
	p := &Provider{cfg: cfg, models: slices.Clone(cfg.Models), discovery: DiscoveryStatus{Status: "off", Count: len(cfg.Models)}}
	if cfg.Discover == nil || !discover {
		return p
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	ids, err := discoverModels(ctx, cfg)
	if err != nil {
		p.discovery = DiscoveryStatus{Status: "failed", Error: err.Error(), Count: len(p.models)}
		slog.Warn("model discovery failed; using the models listed in the config", "provider", cfg.Key, "err", err)
		return p
	}
	p.models = mergeModels(cfg.Models, filterModels(ids, *cfg.Discover))
	p.discovery = DiscoveryStatus{Status: "ok", Count: len(p.models)}
	return p
}

// Register adds one asset per configured provider, discovering models concurrently.
func Register(ctx context.Context, r *registry.Registry, cfg llmconfig.Config, discover bool) {
	providers := cfg.Sorted()
	built := make([]*Provider, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Go(func() { built[i] = NewProvider(ctx, p, discover) })
	}
	wg.Wait()
	for _, p := range built {
		r.MustLLM(p)
	}
}

func (p *Provider) ProviderKey() string        { return p.cfg.Key }
func (p *Provider) Config() llmconfig.Provider { return p.cfg }
func (p *Provider) Discovery() DiscoveryStatus { return p.discovery }
func (p *Provider) CredentialsConfigured() bool {
	return !p.cfg.NeedsKey() || os.Getenv(p.cfg.APIKeyEnv) != ""
}
func (p *Provider) model(id string) (llmconfig.Model, bool) {
	for _, m := range p.models {
		if m.ID == id {
			return m, true
		}
	}
	return llmconfig.Model{}, false
}

func (p *Provider) defaultModel() string {
	if d := p.cfg.DefaultModel(); d != "" {
		if _, ok := p.model(d); ok {
			return d
		}
	}
	if len(p.models) > 0 {
		return p.models[0].ID
	}
	return ""
}

func (p *Provider) Models() []sdk.ModelInfo {
	def := p.defaultModel()
	out := make([]sdk.ModelInfo, len(p.models))
	for i, m := range p.models {
		out[i] = sdk.ModelInfo{ID: m.ID, Label: m.Label, Default: m.ID == def}
	}
	return out
}

func (p *Provider) Manifest() sdk.Manifest {
	secrets := []sdk.SecretRef{}
	if p.cfg.NeedsKey() {
		secrets = append(secrets, sdk.SecretRef{Name: p.cfg.APIKeyEnv, Description: p.cfg.Name + " API key"})
	}
	tags := p.cfg.Tags
	if len(tags) == 0 {
		tags = []string{string(p.cfg.Type)}
	}
	return sdk.Manifest{
		ID:           p.cfg.AssetID(),
		Kind:         sdk.KindLLM,
		Domain:       p.cfg.Domain,
		Name:         p.cfg.Name,
		Description:  p.cfg.Description,
		Owner:        p.cfg.Owner,
		Tags:         tags,
		Lifecycle:    sdk.Lifecycle(p.cfg.Lifecycle),
		ConfigSchema: p.schema(),
		Secrets:      secrets,
		Extra: map[string]any{
			"providerType":          p.cfg.Type,
			"baseURL":               displayURL(p.cfg.BaseURL),
			"apiKeyEnv":             p.cfg.APIKeyEnv,
			"credentialsConfigured": p.CredentialsConfigured(),
			"models":                p.Models(),
			"discovery":             p.discovery,
		},
	}
}

// displayURL drops credentials and query strings before showing a URL.
func displayURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return ""
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

// schema builds the config form: a model picker plus request parameters.
func (p *Provider) schema() *jsonschema.Schema {
	props := jsonschema.NewProperties()
	modelSchema := &jsonschema.Schema{Type: "string", Title: "Model"}
	for _, m := range p.models {
		label := m.Label
		if label == "" {
			label = m.ID
		}
		modelSchema.OneOf = append(modelSchema.OneOf, &jsonschema.Schema{Const: m.ID, Title: label})
	}
	if def := p.defaultModel(); def != "" {
		modelSchema.Default = def
	}
	props.Set("model", modelSchema)
	props.Set("maxTokens", &jsonschema.Schema{Type: "integer", Title: "Max output tokens", Minimum: json.Number("1"), Maximum: json.Number("128000"), Default: p.cfg.Params.MaxTokens})

	levels := []any{"low", "medium", "high"}
	switch p.cfg.Type {
	case llmconfig.TypeAnthropic:
		if slices.ContainsFunc(p.models, func(m llmconfig.Model) bool { return m.Effort }) {
			s := &jsonschema.Schema{Type: "string", Title: "Effort", Enum: levels, Description: "How much the model thinks before answering. Only used by models that support it."}
			if p.cfg.Params.Effort != "" {
				s.Default = p.cfg.Params.Effort
			}
			props.Set("effort", s)
		}
	case llmconfig.TypeOpenAI:
		s := &jsonschema.Schema{Type: "string", Title: "Reasoning effort", Enum: levels, Description: "Only for reasoning models. Leave empty for others."}
		if p.cfg.Params.ReasoningEffort != "" {
			s.Default = p.cfg.Params.ReasoningEffort
		}
		props.Set("reasoningEffort", s)
	}
	return &jsonschema.Schema{Type: "object", Properties: props, AdditionalProperties: jsonschema.FalseSchema}
}

// New creates the ADK model for an agent.
func (p *Provider) New(_ context.Context, raw json.RawMessage, s sdk.Secrets) (model.LLM, error) {
	settings, err := sdk.DecodeConfig(raw, Settings{
		MaxTokens:       p.cfg.Params.MaxTokens,
		Effort:          p.cfg.Params.Effort,
		ReasoningEffort: p.cfg.Params.ReasoningEffort,
	})
	if err != nil {
		return nil, err
	}
	if settings.Model == "" {
		settings.Model = p.defaultModel()
	}
	m, ok := p.model(settings.Model)
	if !ok {
		return nil, fmt.Errorf("model %q is not available on %s", settings.Model, p.cfg.Name)
	}
	if settings.MaxTokens <= 0 {
		settings.MaxTokens = p.cfg.Params.MaxTokens
	}
	var key string
	if p.cfg.NeedsKey() {
		if key, err = s.Get(p.cfg.APIKeyEnv); err != nil {
			return nil, err
		}
	}
	headers := p.cfg.ExpandedHeaders()

	switch p.cfg.Type {
	case llmconfig.TypeAnthropic:
		opts := []anthropicopt.RequestOption{anthropicopt.WithAPIKey(key)}
		if p.cfg.BaseURL != "" {
			opts = append(opts, anthropicopt.WithBaseURL(p.cfg.BaseURL))
		}
		for k, v := range headers {
			opts = append(opts, anthropicopt.WithHeader(k, v))
		}
		am := &anthropicModel{client: anthropic.NewClient(opts...), model: m.ID, maxTokens: settings.MaxTokens, apiKeyEnv: p.cfg.APIKeyEnv}
		if m.Effort {
			am.effort = settings.Effort
		}
		return am, nil
	case llmconfig.TypeOpenAI:
		return &openaiModel{
			client:          openai.NewClient(openaiOptions(p.cfg, key, headers)...),
			model:           m.ID,
			maxTokens:       settings.MaxTokens,
			reasoningEffort: settings.ReasoningEffort,
			streamUsage:     p.cfg.StreamUsageEnabled(),
			provider:        p.cfg.Name,
			apiKeyEnv:       p.cfg.APIKeyEnv,
		}, nil
	}
	return nil, fmt.Errorf("unsupported provider type %q", p.cfg.Type)
}

// openaiOptions sets every connection option explicitly, so OPENAI_* env vars
// can't redirect calls to another endpoint.
func openaiOptions(cfg llmconfig.Provider, key string, headers map[string]string) []openaiopt.RequestOption {
	opts := []openaiopt.RequestOption{openaiopt.WithBaseURL(cfg.BaseURL), openaiopt.WithAPIKey(key)}
	if key == "" {
		// Gateways with auth: none must not receive an Authorization header.
		opts = append(opts, openaiopt.WithHeaderDel("Authorization"))
	}
	for k, v := range headers {
		opts = append(opts, openaiopt.WithHeader(k, v))
	}
	return opts
}
