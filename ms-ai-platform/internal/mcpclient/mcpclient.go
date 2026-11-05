// Package mcpclient discovers the tools an MCP server exposes.
package mcpclient

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agentic-platform/lib-agent-assets/pkg/assets"
)

// Tool is a discovered MCP tool.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

// Transport returns the MCP client transport for a connection.
func Transport(conn assets.MCPConnection) (mcp.Transport, error) {
	if conn.Transport != assets.MCPTransportStreamableHTTP {
		return nil, fmt.Errorf("unsupported MCP transport %q", conn.Transport)
	}
	t := &mcp.StreamableClientTransport{Endpoint: conn.URL}
	if len(conn.Headers) > 0 {
		t.HTTPClient = &http.Client{Transport: headerTransport{headers: conn.Headers, base: http.DefaultTransport}}
	}
	return t, nil
}

type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h.headers {
		r.Header.Set(k, v)
	}
	return h.base.RoundTrip(r)
}

// ListAllowedTools connects to the server and returns its tools that are in the allow-list.
func ListAllowedTools(ctx context.Context, conn assets.MCPConnection) ([]Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	transport, err := Transport(conn)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ms-ai-platform", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", conn.URL, err)
	}
	defer session.Close()
	out := []Tool{}
	for t, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		if slices.Contains(conn.AllowedTools, t.Name) {
			out = append(out, Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
		}
	}
	return out, nil
}
