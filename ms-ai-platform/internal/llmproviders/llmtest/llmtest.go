// Package llmtest builds asset catalogs with LLM providers for tests.
package llmtest

import (
	"context"
	"fmt"
	"strings"

	"agentic-platform/lib-agent-assets/assets"
	"agentic-platform/lib-agent-assets/pkg/registry"
	"agentic-platform/ms-ai-platform/internal/llmconfig"
	"agentic-platform/ms-ai-platform/internal/llmproviders"
)

// Provider describes one registered gateway.
type Provider struct {
	Key     string
	Name    string
	BaseURL string   // e.g. a gatewaytest server URL + "/v1"
	Models  []string // listed in the config; the first is the default
}

// Gateway is the default "gateway" provider (core.llm.gateway) at baseURL.
func Gateway(baseURL string, models ...string) Provider {
	return Provider{Key: "gateway", Name: "LLM Gateway", BaseURL: baseURL, Models: models}
}

// Catalog returns the lib-agent-assets catalog plus the given providers, refreshed
// once from their endpoints (unreachable endpoints keep the configured models).
func Catalog(providers ...Provider) *registry.Registry {
	var b strings.Builder
	b.WriteString("providers:\n")
	for _, p := range providers {
		fmt.Fprintf(&b, "  %s:\n    name: %s\n    baseURL: %s\n    auth: none\n    models:\n", p.Key, p.Name, p.BaseURL)
		for i, m := range p.Models {
			fmt.Fprintf(&b, "      - { id: %q, default: %v }\n", m, i == 0)
		}
	}
	cfg, err := llmconfig.Parse([]byte(b.String()))
	if err != nil {
		panic(err)
	}
	c := assets.New()
	ps := llmproviders.Register(c, cfg)
	(&llmproviders.Refresher{Providers: ps}).RefreshAll(context.Background())
	return c
}
