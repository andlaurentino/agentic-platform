package llmconfig

import (
	"strings"
	"testing"
)

func TestEmbeddedConfig(t *testing.T) {
	t.Setenv("LLM_GATEWAY_URL", "")
	c := Default()
	p, ok := c.Providers["gateway"]
	if !ok || len(c.Providers) != 1 {
		t.Fatalf("want only the gateway provider, got %+v", c.Providers)
	}
	if p.AssetID() != "core.llm.gateway" || p.BaseURL != "http://localhost:8082/v1" || p.KeyRequired() || !p.UsesKey() {
		t.Fatalf("unexpected provider %+v", p)
	}
	t.Setenv("LLM_GATEWAY_API_KEY", "")
	if !p.CredentialsConfigured() {
		t.Error("an optional key must not count as missing")
	}

	t.Setenv("LLM_GATEWAY_URL", "http://ms-llm-gateway:8082/v1")
	if got := Default().Providers["gateway"].BaseURL; got != "http://ms-llm-gateway:8082/v1" {
		t.Fatalf("LLM_GATEWAY_URL not applied: %s", got)
	}
}

func TestExpand(t *testing.T) {
	t.Setenv("SET", "x")
	t.Setenv("EMPTY", "")
	for in, want := range map[string]string{"${SET}": "x", "${SET:-d}": "x", "${EMPTY:-d}": "d", "${UNSET_VAR_FOR_TEST}": "", "a-${UNSET_VAR_FOR_TEST:-b}-c": "a-b-c"} {
		if got := Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"literal key rejected", "providers:\n  a:\n    baseURL: http://x/v1\n    apiKey: sk-123\n", "field apiKey not found"},
		{"key in apiKeyEnv", "providers:\n  a:\n    baseURL: http://x/v1\n    apiKeyEnv: sk-123\n", "environment variable name"},
		{"missing env", "providers:\n  a:\n    baseURL: http://x/v1\n", "apiKeyEnv is required"},
		{"bad url", "providers:\n  a:\n    baseURL: localhost:8082\n    auth: none\n", "http(s) URL"},
		{"two defaults", "providers:\n  a:\n    baseURL: http://x/v1\n    auth: none\n    models: [{id: m, default: true}, {id: n, default: true}]\n", "only one model"},
		{"bad guardrail ref", "defaults: {guardrailModel: nope:m}\nproviders:\n  a:\n    baseURL: http://x/v1\n    auth: none\n", "configured provider"},
		{"bad key", "providers:\n  My_Gateway:\n    baseURL: http://x/v1\n    auth: none\n", "key must be"},
		{"type is gone", "providers:\n  a:\n    type: anthropic\n    baseURL: http://x/v1\n    auth: none\n", "field type not found"},
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

func TestMatchAny(t *testing.T) {
	for id, want := range map[string]bool{"gw/text-embedding-3": true, "claude-haiku/claude-haiku-5-5": false, "a.b?embedding": true} {
		if got := MatchAny([]string{"*embedding*"}, id); got != want {
			t.Errorf("MatchAny(%q) = %v", id, got)
		}
	}
	if !MatchAny([]string{"gpt-4.?"}, "gpt-4.1") || MatchAny([]string{"gpt-4.?"}, "gpt-4x1") || MatchAny([]string{"["}, "x") {
		t.Error("? must match one character and other characters literally")
	}
}
