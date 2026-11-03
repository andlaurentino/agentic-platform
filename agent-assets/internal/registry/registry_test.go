package registry_test

import (
	"encoding/json"
	"strings"
	"testing"

	"agentic-platform/agent-assets/assets"
	"agentic-platform/agent-assets/internal/registry"
	"agentic-platform/agent-assets/sdk"
)

func TestCatalogIsValid(t *testing.T) {
	c := assets.All()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := len(c.Manifests("")); got != 5 {
		t.Fatalf("assets = %d, want 5", got)
	}
}

type badMCP struct{ m sdk.Manifest }

func (b badMCP) Manifest() sdk.Manifest { return b.m }
func (badMCP) Connection(json.RawMessage, sdk.Secrets) (sdk.MCPConnection, error) {
	return sdk.MCPConnection{}, nil
}

func TestValidateCatchesProblems(t *testing.T) {
	good := sdk.Manifest{ID: "core.mcp.x", Kind: sdk.KindMCP, Domain: "core", Name: "X", Description: "d", Owner: "@t",
		Lifecycle: sdk.LifecycleStable, ConfigSchema: sdk.SchemaFor[struct{}]()}
	bad := good
	bad.ID, bad.Owner = "Bad ID", "team"

	r := registry.New()
	r.MustMCP(badMCP{good})
	r.MustMCP(badMCP{good})
	r.MustMCP(badMCP{bad})
	err := r.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"duplicate asset id", "{domain}.{kind}.{name}", "owner must be"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	type cfg struct {
		A string `json:"a,omitempty"`
		B int    `json:"b,omitempty"`
	}
	got, err := sdk.DecodeConfig(json.RawMessage(`{"b":2}`), cfg{A: "x", B: 1})
	if err != nil || got.A != "x" || got.B != 2 {
		t.Fatalf("got %+v %v", got, err)
	}
}
