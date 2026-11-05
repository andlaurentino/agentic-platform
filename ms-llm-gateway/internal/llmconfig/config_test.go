package llmconfig

import (
	"strings"
	"testing"
	"time"
)

func TestEmbeddedConfig(t *testing.T) {
	c := Default()
	p, ok := c.Providers["claude-haiku"]
	if !ok {
		t.Fatal("claude-haiku provider missing")
	}
	if p.Type != TypeAnthropic || p.APIKeyEnv != "ANTHROPIC_API_KEY" || c.Defaults.Model != "claude-haiku/claude-haiku-5-5" {
		t.Fatalf("unexpected config %+v", c)
	}
	if m, _ := p.Model("claude-haiku-5-5"); !m.Effort || !m.Default {
		t.Error("haiku 5.5 should be the default and accept effort")
	}
}

const upstream = `
defaults:
  model: gw/gpt-4.1-mini
providers:
  gw:
    type: openai
    baseURL: http://localhost:4000/v1
    apiKeyEnv: GW_KEY
    headers: { X-Team: "${TEAM_ID}" }
    discover: { include: ["gpt-*"], exclude: ["*embedding*"], refresh: 30s }
`

func TestParseUpstream(t *testing.T) {
	c, err := Parse([]byte(upstream))
	if err != nil {
		t.Fatal(err)
	}
	p := c.Providers["gw"]
	if p.Name != "gw" || p.Params.MaxTokens != 4096 || !p.StreamUsageEnabled() || p.RefreshInterval() != 30*time.Second {
		t.Fatalf("defaults not applied: %+v", p)
	}
	t.Setenv("TEAM_ID", "payments")
	if h := p.ExpandedHeaders(); h["X-Team"] != "payments" {
		t.Fatalf("headers = %v", h)
	}
	t.Setenv("GW_KEY", "")
	if p.CredentialsConfigured() {
		t.Error("empty key env should not count as configured")
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
		{"discover on anthropic", "providers:\n  a:\n    type: anthropic\n    auth: none\n    discover: {}\n    models: [{id: m}]\n", "only supported for openai"},
		{"bad default", "defaults: {model: nope/m}\nproviders:\n  a:\n    type: anthropic\n    auth: none\n    models: [{id: m}]\n", "configured provider"},
		{"default without provider", "defaults: {model: m}\nproviders:\n  a:\n    type: anthropic\n    auth: none\n    models: [{id: m}]\n", "<provider>/<model>"},
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
