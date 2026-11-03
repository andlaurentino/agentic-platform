// Package catalog serves the compiled-in asset catalog to the UI and the runtime.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"google.golang.org/adk/model"

	"agentic-platform/agent-assets/assets"
	"agentic-platform/agent-assets/assets/core/llms"
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

// Deps returns the services guardrails can use. L2 guardrails get models from the Claude Haiku asset.
func (c *Catalog) Deps() sdk.Deps {
	return sdk.Deps{
		Secrets: c.Secrets,
		LLM: func(ctx context.Context, modelID string) (model.LLM, error) {
			llm, ok := c.LLM(llms.ClaudeHaikuID)
			if !ok {
				return nil, fmt.Errorf("LLM asset %s is not registered", llms.ClaudeHaikuID)
			}
			cfg, _ := json.Marshal(map[string]any{"model": modelID, "maxTokens": 512, "effort": "low"})
			return llm.New(ctx, cfg, c.Secrets)
		},
	}
}

// Routes registers the catalog endpoints.
func (c *Catalog) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, c.Manifests(sdk.Kind(r.URL.Query().Get("kind"))))
	})
	mux.HandleFunc("GET /v1/catalog/{id}", func(w http.ResponseWriter, r *http.Request) {
		a, ok := c.Get(r.PathValue("id"))
		if !ok {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "asset not found", nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, a.Manifest())
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
