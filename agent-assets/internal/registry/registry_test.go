package registry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentic-platform/agent-assets/assets"
	"agentic-platform/agent-assets/internal/registry"
	"agentic-platform/agent-assets/llmconfig"
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

const twoProviders = `
defaults: { guardrailModel: "gw:fast" }
providers:
  claude-haiku:
    type: anthropic
    apiKeyEnv: ANTHROPIC_API_KEY
    models: [{ id: claude-haiku-5-5, label: Claude Haiku 5.5 }]
  gw:
    name: Gateway
    type: openai
    baseURL: http://localhost:4000/v1
    auth: none
    models: [{ id: fast, label: Fast }, { id: claude-haiku-5-5 }]
`

func TestModelRefs(t *testing.T) {
	cfg, err := llmconfig.Parse([]byte(twoProviders))
	if err != nil {
		t.Fatal(err)
	}
	c := assets.New(context.Background(), assets.WithLLMConfig(cfg))
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, r := range c.ModelRefs() {
		refs = append(refs, r.Ref)
	}
	if got := strings.Join(refs, ","); got != "claude-haiku:claude-haiku-5-5,gw:fast,gw:claude-haiku-5-5" {
		t.Fatalf("refs = %s", got)
	}

	for ref, want := range map[string]string{"gw:claude-haiku-5-5": "gw", "claude-haiku-5-5": "claude-haiku", "fast": "gw"} {
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

	// The prompt-injection guardrail's model field lists every configured model.
	m, _ := c.Manifest("core.guardrail.prompt-injection")
	b, _ := json.Marshal(m.ConfigSchema)
	if !strings.Contains(string(b), `"const":"gw:fast"`) || !strings.Contains(string(b), `Gateway · Fast`) {
		t.Fatalf("guardrail schema not enriched: %s", b)
	}
	if c.DefaultModel() != "gw:fast" {
		t.Fatalf("default model = %s", c.DefaultModel())
	}
}
