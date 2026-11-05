// Package mcps holds MCP server contracts.
package mcps

import (
	"encoding/json"
	"os"

	"agentic-platform/lib-agent-assets/pkg/assets"
)

const DemoID = "core.mcp.demo"

// DemoAllowedTools are the demo-mcp tools agents may use.
var DemoAllowedTools = []string{"get_weather", "lookup_order", "search_faq"}

type DemoConfig struct {
	URL string `json:"url,omitempty" jsonschema:"title=Server URL,format=uri" jsonschema_description:"Overrides DEMO_MCP_URL. Leave empty to use the platform default."`
}

// Demo is the contract for the demo-mcp server in this repo.
type Demo struct{}

func (Demo) Manifest() assets.Manifest {
	return assets.Manifest{
		ID:           DemoID,
		Kind:         assets.KindMCP,
		Domain:       "core",
		Name:         "Demo MCP",
		Description:  "Example MCP server with weather, order lookup and FAQ search tools (fake data).",
		Owner:        "@ai-platform",
		Tags:         []string{"demo"},
		Lifecycle:    assets.LifecycleExperimental,
		ConfigSchema: assets.SchemaFor[DemoConfig](),
		Extra: map[string]any{
			"transport":    assets.MCPTransportStreamableHTTP,
			"auth":         "none",
			"allowedTools": DemoAllowedTools,
		},
	}
}

func (Demo) Connection(raw json.RawMessage, _ assets.Secrets) (assets.MCPConnection, error) {
	url := os.Getenv("DEMO_MCP_URL")
	if url == "" {
		url = "http://localhost:8081/mcp"
	}
	cfg, err := assets.DecodeConfig(raw, DemoConfig{URL: url})
	if err != nil {
		return assets.MCPConnection{}, err
	}
	if cfg.URL == "" {
		cfg.URL = url
	}
	return assets.MCPConnection{URL: cfg.URL, Transport: assets.MCPTransportStreamableHTTP, AllowedTools: DemoAllowedTools}, nil
}
