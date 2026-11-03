// Package assets aggregates every domain's assets into one catalog.
package assets

import (
	"context"

	"agentic-platform/agent-assets/assets/core"
	"agentic-platform/agent-assets/assets/core/llms"
	"agentic-platform/agent-assets/internal/registry"
	"agentic-platform/agent-assets/llmconfig"
)

// Catalog is the registry type exposed to the platform.
type Catalog = registry.Registry

// ModelRef identifies one model of one provider ("<provider>:<model>").
type ModelRef = registry.ModelRef

type options struct {
	llm      *llmconfig.Config
	discover bool
}

// Option configures New.
type Option func(*options)

// WithLLMConfig uses cfg instead of the embedded llms.yaml.
func WithLLMConfig(cfg llmconfig.Config) Option { return func(o *options) { o.llm = &cfg } }

// WithDiscovery lets OpenAI-compatible providers list their models from the endpoint.
func WithDiscovery(on bool) Option { return func(o *options) { o.discover = on } }

// New builds the catalog: LLM providers from the config, then every domain's assets.
func New(ctx context.Context, opts ...Option) *Catalog {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	cfg := llmconfig.Default()
	if o.llm != nil {
		cfg = *o.llm
	}
	r := registry.New()
	llms.Register(ctx, r, cfg, o.discover)
	r.SetDefaultModel(cfg.Defaults.GuardrailModel)
	core.Register(r)
	return r
}

// All returns the catalog with the embedded llms.yaml and no network discovery.
func All() *Catalog {
	return New(context.Background())
}
