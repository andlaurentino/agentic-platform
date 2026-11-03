package sdk

import (
	"context"
	"encoding/json"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
)

// Asset is implemented by every asset kind.
type Asset interface {
	Manifest() Manifest
}

// LLM connects to a model provider and returns an ADK model.
type LLM interface {
	Asset
	New(ctx context.Context, cfg json.RawMessage, s Secrets) (model.LLM, error)
}

// ModelInfo describes one model an LLM provider offers.
type ModelInfo struct {
	ID      string `json:"id"`
	Label   string `json:"label,omitempty"`
	Default bool   `json:"default,omitempty"`
}

// ModelProvider is an LLM asset that offers several models, selected with the
// "model" config field. ProviderKey is used in model references
// ("<provider>:<model>") such as a guardrail's classifier model.
type ModelProvider interface {
	LLM
	ProviderKey() string
	Models() []ModelInfo
}

// Tool is a native Go function exposed to agents as an ADK tool.
type Tool interface {
	Asset
	New(ctx context.Context, cfg json.RawMessage, s Secrets) (tool.Tool, error)
}

// MCPTransport is how the platform reaches an MCP server.
type MCPTransport string

const MCPTransportStreamableHTTP MCPTransport = "streamable-http"

// MCPConnection is the resolved contract the platform uses to connect to an MCP server.
type MCPConnection struct {
	URL          string            `json:"url"`
	Transport    MCPTransport      `json:"transport"`
	Headers      map[string]string `json:"-"`
	AllowedTools []string          `json:"allowedTools"`
}

// MCP is the contract for an MCP server.
type MCP interface {
	Asset
	Connection(cfg json.RawMessage, s Secrets) (MCPConnection, error)
}
