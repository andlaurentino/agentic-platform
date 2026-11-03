// Package mcps holds MCP server contracts.
package mcps

import (
	"encoding/json"
	"os"

	"agentic-platform/agent-assets/sdk"
)

const DemoID = "core.mcp.demo"

// DemoAllowedTools are the demo-mcp tools agents may use.
var DemoAllowedTools = []string{"get_weather", "lookup_order", "search_faq"}

type DemoConfig struct {
	URL string `json:"url,omitempty" jsonschema:"title=Server URL,format=uri" jsonschema_description:"Overrides DEMO_MCP_URL. Leave empty to use the platform default."`
}

// Demo is the contract for the demo-mcp server in this repo.
type Demo struct{}

func (Demo) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:           DemoID,
		Kind:         sdk.KindMCP,
		Domain:       "core",
		Name:         "Demo MCP",
		Description:  "Example MCP server with weather, order lookup and FAQ search tools (fake data).",
		Owner:        "@ai-platform",
		Tags:         []string{"demo"},
		Lifecycle:    sdk.LifecycleExperimental,
		ConfigSchema: sdk.SchemaFor[DemoConfig](),
		Extra: map[string]any{
			"transport":    sdk.MCPTransportStreamableHTTP,
			"auth":         "none",
			"allowedTools": DemoAllowedTools,
		},
	}
}

func (Demo) Connection(raw json.RawMessage, _ sdk.Secrets) (sdk.MCPConnection, error) {
	url := os.Getenv("DEMO_MCP_URL")
	if url == "" {
		url = "http://localhost:8081/mcp"
	}
	cfg, err := sdk.DecodeConfig(raw, DemoConfig{URL: url})
	if err != nil {
		return sdk.MCPConnection{}, err
	}
	if cfg.URL == "" {
		cfg.URL = url
	}
	return sdk.MCPConnection{URL: cfg.URL, Transport: sdk.MCPTransportStreamableHTTP, AllowedTools: DemoAllowedTools}, nil
}
