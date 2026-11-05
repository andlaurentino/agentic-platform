// Package llmproviders turns the OpenAI-compatible endpoints in
// llm-providers.yaml (ms-llm-gateway by default) into LLM assets. Each asset
// keeps the endpoint's model list fresh and creates ADK models that call it.
package llmproviders

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sync/atomic"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/model"

	"agentic-platform/lib-agent-assets/pkg/assets"
	"agentic-platform/lib-agent-assets/pkg/registry"
	"agentic-platform/ms-ai-platform/internal/llmconfig"
)

// Settings are what builders set on an agent that uses a provider.
type Settings struct {
	Model     string `json:"model,omitempty"`
	MaxTokens int64  `json:"maxTokens,omitempty"`
	Effort    string `json:"effort,omitempty"`
}

// Status reports whether the provider's model list could be read.
type Status struct {
	State       string    `json:"status"` // pending | ok | failed
	Error       string    `json:"error,omitempty"`
	Models      int       `json:"models"`
	RefreshedAt time.Time `json:"refreshedAt,omitzero"`
}

// snapshot is replaced as a whole on every refresh.
type snapshot struct {
	models []assets.ModelInfo
	status Status
}

// Provider is the LLM asset for one llm-providers.yaml entry.
type Provider struct {
	cfg  llmconfig.Provider
	snap atomic.Pointer[snapshot]
}

var _ assets.ModelProvider = (*Provider)(nil)

// New builds the asset. Until the first refresh its models are the ones listed
// in the config.
func New(cfg llmconfig.Provider) *Provider {
	p := &Provider{cfg: cfg}
	models := configuredModels(cfg)
	p.snap.Store(&snapshot{models: models, status: Status{State: "pending", Models: len(models)}})
	return p
}

// Register adds one asset per configured provider and sets the default
// guardrail model: defaults.guardrailModel, else the first provider's default.
func Register(r *registry.Registry, cfg llmconfig.Config) []*Provider {
	var ps []*Provider
	for _, pc := range cfg.Sorted() {
		p := New(pc)
		r.MustLLM(p)
		ps = append(ps, p)
	}
	r.SetDefaultModel(func() string {
		if ref := cfg.Defaults.GuardrailModel; ref != "" {
			return ref
		}
		for _, p := range ps {
			if def := p.defaultModel(); def != "" {
				return p.cfg.Key + ":" + def
			}
		}
		return ""
	})
	return ps
}

func configuredModels(cfg llmconfig.Provider) []assets.ModelInfo {
	out := make([]assets.ModelInfo, len(cfg.Models))
	for i, m := range cfg.Models {
		out[i] = assets.ModelInfo{ID: m.ID, Label: m.Label, Default: m.Default}
	}
	return out
}

func (p *Provider) ProviderKey() string         { return p.cfg.Key }
func (p *Provider) Config() llmconfig.Provider  { return p.cfg }
func (p *Provider) Status() Status              { return p.snap.Load().status }
func (p *Provider) CredentialsConfigured() bool { return p.cfg.CredentialsConfigured() }

// Models returns the current models; exactly one is marked default when any exist.
func (p *Provider) Models() []assets.ModelInfo {
	models := slices.Clone(p.snap.Load().models)
	def := p.defaultModel()
	for i := range models {
		models[i].Default = models[i].ID == def
	}
	return models
}

// defaultModel is the configured default, else the one the endpoint marks
// default (an ms-llm-gateway extension), else the first.
func (p *Provider) defaultModel() string {
	models := p.snap.Load().models
	for _, m := range p.cfg.Models {
		if m.Default && slices.ContainsFunc(models, func(x assets.ModelInfo) bool { return x.ID == m.ID }) {
			return m.ID
		}
	}
	for _, m := range models {
		if m.Default {
			return m.ID
		}
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
}

func (p *Provider) hasModel(id string) bool {
	return slices.ContainsFunc(p.snap.Load().models, func(m assets.ModelInfo) bool { return m.ID == id })
}

func (p *Provider) Manifest() assets.Manifest {
	var secrets []assets.SecretRef
	if p.cfg.UsesKey() {
		secrets = append(secrets, assets.SecretRef{Name: p.cfg.APIKeyEnv, Description: p.cfg.Name + " API key"})
	}
	extra := map[string]any{
		"baseURL":               displayURL(p.cfg.BaseURL),
		"credentialsConfigured": p.CredentialsConfigured(),
		"models":                p.Models(),
		"status":                p.Status(),
	}
	if len(p.snap.Load().models) == 0 {
		extra["problem"] = p.unavailable().Error()
	}
	if p.cfg.UsesKey() {
		extra["apiKeyEnv"] = p.cfg.APIKeyEnv
		extra["apiKeyRequired"] = p.cfg.KeyRequired()
	}
	return assets.Manifest{
		ID:           p.cfg.AssetID(),
		Kind:         assets.KindLLM,
		Domain:       "core",
		Name:         p.cfg.Name,
		Description:  p.cfg.Description,
		Owner:        "@ai-platform",
		Tags:         []string{"openai-compatible"},
		Lifecycle:    assets.LifecycleStable,
		ConfigSchema: p.schema(),
		Secrets:      secrets,
		Extra:        extra,
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
	for _, m := range p.Models() {
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
	effort := &jsonschema.Schema{Type: "string", Title: "Reasoning effort", Enum: []any{"low", "medium", "high"},
		Description: "How much the model thinks before answering. Ignored by models that don't support it."}
	if p.cfg.Params.Effort != "" {
		effort.Default = p.cfg.Params.Effort
	}
	props.Set("effort", effort)
	return &jsonschema.Schema{Type: "object", Properties: props, AdditionalProperties: jsonschema.FalseSchema}
}

// New creates the ADK model for an agent.
func (p *Provider) New(_ context.Context, raw json.RawMessage, s assets.Secrets) (model.LLM, error) {
	settings, err := assets.DecodeConfig(raw, Settings{MaxTokens: p.cfg.Params.MaxTokens, Effort: p.cfg.Params.Effort})
	if err != nil {
		return nil, err
	}
	if settings.Model == "" {
		settings.Model = p.defaultModel()
	}
	if settings.Model == "" {
		return nil, p.unavailable()
	}
	if !p.hasModel(settings.Model) {
		return nil, fmt.Errorf("model %q is not available on %s", settings.Model, p.cfg.Name)
	}
	if settings.MaxTokens <= 0 {
		settings.MaxTokens = p.cfg.Params.MaxTokens
	}
	key, err := p.apiKey(s)
	if err != nil {
		return nil, err
	}
	return &openaiModel{
		client:          openai.NewClient(p.clientOptions(key)...),
		model:           settings.Model,
		maxTokens:       settings.MaxTokens,
		reasoningEffort: settings.Effort,
		streamUsage:     p.cfg.StreamUsageEnabled(),
		provider:        p.cfg.Name,
		baseURL:         displayURL(p.cfg.BaseURL),
		apiKeyEnv:       p.cfg.APIKeyEnv,
	}, nil
}

// unavailable explains why a provider has no models.
func (p *Provider) unavailable() error {
	st := p.Status()
	switch st.State {
	case "failed":
		return fmt.Errorf("%s is unavailable: %s", p.cfg.Name, st.Error)
	case "pending":
		return fmt.Errorf("%s has not been reached yet; is it running at %s?", p.cfg.Name, displayURL(p.cfg.BaseURL))
	}
	return fmt.Errorf("%s offers no models; check its configuration or include/exclude filters", p.cfg.Name)
}

func (p *Provider) apiKey(s assets.Secrets) (string, error) {
	if !p.cfg.UsesKey() {
		return "", nil
	}
	key, err := s.Get(p.cfg.APIKeyEnv)
	if err != nil && p.cfg.KeyRequired() {
		return "", fmt.Errorf("%s: %w", p.cfg.Name, err)
	}
	return key, nil
}

// clientOptions sets every connection option explicitly, so OPENAI_* env vars
// can't redirect calls to another endpoint.
func (p *Provider) clientOptions(key string) []option.RequestOption {
	// No client retries: ms-llm-gateway already retries its upstreams, and
	// retrying here would multiply the wait on every failure.
	opts := []option.RequestOption{option.WithBaseURL(p.cfg.BaseURL), option.WithAPIKey(key), option.WithMaxRetries(0)}
	if key == "" {
		// Endpoints without auth must not receive an empty Authorization header.
		opts = append(opts, option.WithHeaderDel("Authorization"))
	}
	for k, v := range p.cfg.ExpandedHeaders() {
		opts = append(opts, option.WithHeader(k, v))
	}
	return opts
}
