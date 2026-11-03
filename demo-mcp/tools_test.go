package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerOverStreamableHTTP(t *testing.T) {
	server := NewServer()
	srv := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	defer srv.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
	}
	if got := strings.Join(names, ","); got != "get_weather,lookup_order,search_faq" {
		t.Fatalf("tools = %s", got)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_weather", Arguments: map[string]any{"city": "Lisbon"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || res.StructuredContent == nil {
		t.Fatalf("unexpected result %+v", res)
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "lookup_order", Arguments: map[string]any{"order_id": "nope"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for unknown order")
	}
}
