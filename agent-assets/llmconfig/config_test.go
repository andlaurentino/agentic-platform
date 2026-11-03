package llmconfig

import (
	"strings"
	"testing"
)

func TestEmbeddedConfig(t *testing.T) {
	c := Default()
	p, ok := c.Providers["claude-haiku"]
	if !ok {
		t.Fatal("claude-haiku provider missing")
	}
	if p.AssetID() != "core.llm.claude-haiku" || p.DefaultModel() != "claude-haiku-5-5" || p.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Fatalf("unexpected provider %+v", p)
	}
	if m, _ := p.Model("claude-haiku-5-5"); !m.Effort {
		t.Error("haiku 5.5 should accept effort")
	}
}

const gateway = `
defaults:
  guardrailModel: gw:gpt-4.1-mini
providers:
  gw:
    type: openai
    baseURL: http://localhost:4000/v1
    apiKeyEnv: GW_KEY
    headers: { X-Team: "${TEAM_ID}" }
    discover: { include: ["gpt-*"], exclude: ["*embedding*"] }
`

func TestParseGateway(t *testing.T) {
	c, err := Parse([]byte(gateway))
	if err != nil {
		t.Fatal(err)
	}
	p := c.Providers["gw"]
	if p.Domain != "core" || p.Name != "gw" || p.Params.MaxTokens != 4096 || !p.StreamUsageEnabled() {
		t.Fatalf("defaults not applied: %+v", p)
	}
	t.Setenv("TEAM_ID", "payments")
	if h := p.ExpandedHeaders(); h["X-Team"] != "payments" {
		t.Fatalf("headers = %v", h)
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"literal key rejected", "providers:\n  a:\n    type: anthropic\n    apiKey: sk-123\n    models: [{id: m}]\n", "field apiKey not found"},
		{"key in apiKeyEnv", "providers:\n  a:\n    type: anthropic\n    apiKeyEnv: sk-ant-123\n    models: [{id: m}]\n", "environment variable name"},
		{"missing env", "providers:\n  a:\n    type: anthropic\n    models: [{id: m}]\n", "apiKeyEnv is required"},
		{"bad type", "providers:\n  a:\n    type: gemini\n    auth: none\n    models: [{id: m}]\n", "type must be"},
		{"openai needs url", "providers:\n  a:\n    type: openai\n    auth: none\n    models: [{id: m}]\n", "baseURL is required"},
		{"no models", "providers:\n  a:\n    type: anthropic\n    auth: none\n", "at least one model"},
		{"two defaults", "providers:\n  a:\n    type: anthropic\n    auth: none\n    models: [{id: m, default: true}, {id: n, default: true}]\n", "only one model"},
		{"bad glob", "providers:\n  a:\n    type: openai\n    auth: none\n    baseURL: http://x/v1\n    discover: {include: ['[']}\n", "invalid glob"},
		{"discover on anthropic", "providers:\n  a:\n    type: anthropic\n    auth: none\n    discover: {}\n    models: [{id: m}]\n", "only supported for openai"},
		{"bad guardrail ref", "defaults: {guardrailModel: nope:m}\nproviders:\n  a:\n    type: anthropic\n    auth: none\n    models: [{id: m}]\n", "provider \"nope\""},
		{"bad key", "providers:\n  My_Gateway:\n    type: anthropic\n    auth: none\n    models: [{id: m}]\n", "key must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestSplitRef(t *testing.T) {
	if p, m := SplitRef("gw:gpt-4.1"); p != "gw" || m != "gpt-4.1" {
		t.Fatal(p, m)
	}
	if p, m := SplitRef("claude-haiku-5-5"); p != "" || m != "claude-haiku-5-5" {
		t.Fatal(p, m)
	}
}
