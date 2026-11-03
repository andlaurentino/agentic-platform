// Package catalog serves the compiled-in asset catalog to the UI and the runtime.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"google.golang.org/adk/model"

	"agentic-platform/agent-assets/assets"
	"agentic-platform/agent-assets/sdk"
	"agentic-platform/ms-ai-platform/internal/httpx"
	"agentic-platform/ms-ai-platform/internal/mcpclient"
)

// Catalog wraps the asset registry with platform services.
type Catalog struct {
	*assets.Catalog
	Secrets sdk.Secrets
}

func New(c *assets.Catalog, s sdk.Secrets) *Catalog {
	return &Catalog{Catalog: c, Secrets: s}
}

// Deps returns the services guardrails can use. L2 guardrails get models by
// reference ("<provider>:<model>"); an empty reference means the default from llms.yaml.
func (c *Catalog) Deps() sdk.Deps {
	return sdk.Deps{
		Secrets: c.Secrets,
		LLM: func(ctx context.Context, ref string) (model.LLM, error) {
			if ref == "" {
				ref = c.DefaultModel()
			}
			if ref == "" {
				return nil, fmt.Errorf("no guardrail model configured (set defaults.guardrailModel in llms.yaml)")
			}
			provider, modelID, err := c.ResolveModel(ref)
			if err != nil {
				return nil, err
			}
			cfg, _ := json.Marshal(map[string]any{"model": modelID, "maxTokens": 512, "effort": "low"})
			return provider.New(ctx, cfg, c.Secrets)
		},
	}
}

// ProviderStatus is the health of one LLM provider.
type ProviderStatus struct {
	ID                    string `json:"id"`
	Type                  string `json:"type"`
	CredentialsConfigured bool   `json:"credentialsConfigured"`
	Models                int    `json:"models"`
	Discovery             any    `json:"discovery"`
}

// Providers reports every LLM provider's credentials, model count and discovery result.
func (c *Catalog) Providers() []ProviderStatus {
	out := []ProviderStatus{}
	for _, m := range c.Manifests(sdk.KindLLM) {
		models, _ := m.Extra["models"].([]sdk.ModelInfo)
		creds, _ := m.Extra["credentialsConfigured"].(bool)
		out = append(out, ProviderStatus{
			ID: m.ID, Type: fmt.Sprint(m.Extra["providerType"]), CredentialsConfigured: creds,
			Models: len(models), Discovery: m.Extra["discovery"],
		})
	}
	return out
}

// Routes registers the catalog endpoints.
func (c *Catalog) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, c.Manifests(sdk.Kind(r.URL.Query().Get("kind"))))
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
