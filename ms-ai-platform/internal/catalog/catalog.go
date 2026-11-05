// Package catalog serves the compiled-in asset catalog to the UI and the runtime.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"google.golang.org/adk/model"

	"agentic-platform/lib-agent-assets/pkg/assets"
	"agentic-platform/lib-agent-assets/pkg/registry"
	"agentic-platform/ms-ai-platform/internal/httpx"
	"agentic-platform/ms-ai-platform/internal/llmproviders"
	"agentic-platform/ms-ai-platform/internal/mcpclient"
)

// Catalog wraps the asset registry with platform services.
type Catalog struct {
	*registry.Registry
	Secrets assets.Secrets
}

func New(c *registry.Registry, s assets.Secrets) *Catalog {
	return &Catalog{Registry: c, Secrets: s}
}

// Deps returns the services guardrails can use. L2 guardrails get models by
// reference ("<provider>:<model>"); an empty reference means the platform default.
func (c *Catalog) Deps() assets.Deps {
	return assets.Deps{
		Secrets: c.Secrets,
		LLM: func(ctx context.Context, ref string) (model.LLM, error) {
			if ref == "" {
				ref = c.DefaultModel()
			}
			if ref == "" {
				return nil, fmt.Errorf("no guardrail model available: no LLM provider offers a model (set defaults.guardrailModel in llm-providers.yaml)")
			}
			provider, modelID, err := c.ResolveModel(ref)
			if err != nil {
				return nil, err
			}
			cfg, _ := json.Marshal(map[string]any{"model": modelID, "maxTokens": 512})
			return provider.New(ctx, cfg, c.Secrets)
		},
	}
}

// ProviderStatus is the health of one LLM provider (gateway).
type ProviderStatus struct {
	ID                    string `json:"id"`
	BaseURL               string `json:"baseURL"`
	CredentialsConfigured bool   `json:"credentialsConfigured"`
	llmproviders.Status
}

// Providers reports every LLM provider's endpoint, credentials and model list status.
func (c *Catalog) Providers() []ProviderStatus {
	out := []ProviderStatus{}
	for _, m := range c.Manifests(assets.KindLLM) {
		creds, _ := m.Extra["credentialsConfigured"].(bool)
		status, _ := m.Extra["status"].(llmproviders.Status)
		baseURL, _ := m.Extra["baseURL"].(string)
		out = append(out, ProviderStatus{ID: m.ID, BaseURL: baseURL, CredentialsConfigured: creds, Status: status})
	}
	return out
}

// Routes registers the catalog endpoints.
func (c *Catalog) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, c.Manifests(assets.Kind(r.URL.Query().Get("kind"))))
	})
	mux.HandleFunc("GET /v1/catalog/{id}", func(w http.ResponseWriter, r *http.Request) {
		m, ok := c.Manifest(r.PathValue("id"))
		if !ok {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "asset not found", nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, m)
	})
	mux.HandleFunc("GET /v1/mcps/{id}/tools", func(w http.ResponseWriter, r *http.Request) {
		m, ok := c.MCP(r.PathValue("id"))
		if !ok {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "MCP asset not found", nil)
			return
		}
		conn, err := m.Connection(nil, c.Secrets)
		if err != nil {
			httpx.WriteError(w, http.StatusBadGateway, "mcp_config", err.Error(), nil)
			return
		}
		tools, err := mcpclient.ListAllowedTools(r.Context(), conn)
		if err != nil {
			httpx.WriteError(w, http.StatusBadGateway, "mcp_unreachable", err.Error(), map[string]string{"url": conn.URL})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, tools)
	})
}
