package registry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/adk/model"

	allassets "agentic-platform/lib-agent-assets/assets"
	"agentic-platform/lib-agent-assets/pkg/assets"
	"agentic-platform/lib-agent-assets/pkg/registry"
)

func TestCatalogIsValid(t *testing.T) {
	c := allassets.New()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := len(c.Manifests("")); got != 4 {
		t.Fatalf("assets = %d, want 4", got)
	}
}

type badMCP struct{ m assets.Manifest }

func (b badMCP) Manifest() assets.Manifest { return b.m }
func (badMCP) Connection(json.RawMessage, assets.Secrets) (assets.MCPConnection, error) {
	return assets.MCPConnection{}, nil
}

func TestValidateCatchesProblems(t *testing.T) {
	good := assets.Manifest{ID: "core.mcp.x", Kind: assets.KindMCP, Domain: "core", Name: "X", Description: "d", Owner: "@t",
		Lifecycle: assets.LifecycleStable, ConfigSchema: assets.SchemaFor[struct{}]()}
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
	got, err := assets.DecodeConfig(json.RawMessage(`{"b":2}`), cfg{A: "x", B: 1})
	if err != nil || got.A != "x" || got.B != 2 {
		t.Fatalf("got %+v %v", got, err)
	}
}

// fakeProvider is an LLM provider asset like the ones the platform registers.
type fakeProvider struct {
	key, name string
	models    []assets.ModelInfo
}

func (f fakeProvider) Manifest() assets.Manifest {
	return assets.Manifest{ID: "core.llm." + f.key, Kind: assets.KindLLM, Domain: "core", Name: f.name, Description: "d", Owner: "@t",
		Lifecycle: assets.LifecycleStable, ConfigSchema: assets.SchemaFor[struct{}]()}
}
func (fakeProvider) New(context.Context, json.RawMessage, assets.Secrets) (model.LLM, error) {
	return nil, nil
}
func (f fakeProvider) ProviderKey() string        { return f.key }
func (f fakeProvider) Models() []assets.ModelInfo { return f.models }

func TestModelRefs(t *testing.T) {
	c := allassets.New()
	c.MustLLM(fakeProvider{key: "gateway", name: "LLM Gateway", models: []assets.ModelInfo{
		{ID: "claude-haiku/claude-haiku-5-5", Label: "Claude Haiku 5.5"}, {ID: "llama3:8b"},
	}})
	c.MustLLM(fakeProvider{key: "gw", name: "Gateway", models: []assets.ModelInfo{{ID: "fast", Label: "Fast"}, {ID: "llama3:8b"}}})
	c.SetDefaultModel(func() string { return "gw:fast" })
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, r := range c.ModelRefs() {
		refs = append(refs, r.Ref)
	}
	if got := strings.Join(refs, ","); got != "gateway:claude-haiku/claude-haiku-5-5,gateway:llama3:8b,gw:fast,gw:llama3:8b" {
		t.Fatalf("refs = %s", got)
	}

	for ref, want := range map[string]string{"gw:llama3:8b": "gw", "llama3:8b": "gateway", "fast": "gw", "gateway:claude-haiku/claude-haiku-5-5": "gateway"} {
		p, model, err := c.ResolveModel(ref)
		if err != nil || p.ProviderKey() != want {
			t.Errorf("ResolveModel(%q) = %v %s %v, want provider %s", ref, p, model, err, want)
		}
	}
	for _, bad := range []string{"gw:nope", "nope:fast", "nope"} {
		if _, _, err := c.ResolveModel(bad); err == nil {
			t.Errorf("ResolveModel(%q) should fail", bad)
		}
	}

	// The prompt-injection guardrail's model field lists every provider's models.
	m, _ := c.Manifest("core.guardrail.prompt-injection")
	b, _ := json.Marshal(m.ConfigSchema)
	if !strings.Contains(string(b), `"const":"gw:fast"`) || !strings.Contains(string(b), `Gateway · Fast`) || !strings.Contains(string(b), "platform default (gw:fast)") {
		t.Fatalf("guardrail schema not enriched: %s", b)
	}
}
