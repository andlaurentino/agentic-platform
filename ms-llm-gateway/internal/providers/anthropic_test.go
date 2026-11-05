package providers

import (
	"encoding/json"
	"strings"
	"testing"

	"agentic-platform/ms-llm-gateway/internal/llmconfig"
	"agentic-platform/ms-llm-gateway/internal/wire"
)

func paramsJSON(t *testing.T, req string, model llmconfig.Model) map[string]any {
	t.Helper()
	var r wire.ChatRequest
	if err := json.Unmarshal([]byte(req), &r); err != nil {
		t.Fatal(err)
	}
	cfg := llmconfig.Default().Providers["claude-haiku"]
	params, werr := anthropicParams(Call{Model: model, Req: &r}, cfg)
	if werr != nil {
		t.Fatal(werr)
	}
	b, _ := json.Marshal(params)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func TestAnthropicRequestConversion(t *testing.T) {
	got := paramsJSON(t, `{
		"model": "claude-haiku/claude-haiku-5-5",
		"max_completion_tokens": 300,
		"reasoning_effort": "medium",
		"messages": [
			{"role": "system", "content": "Be brief."},
			{"role": "user", "content": [{"type": "text", "text": "Weather in Lisbon?"}]},
			{"role": "assistant", "content": null,
			 "thinking_blocks": [{"type": "thinking", "thinking": "need a tool", "signature": "sig-1"}],
			 "tool_calls": [{"id": "t1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Lisbon\"}"}}]},
			{"role": "tool", "tool_call_id": "t1", "content": "{\"temp\":21}"},
			{"role": "user", "content": "And tomorrow?"}
		],
		"tools": [{"type": "function", "function": {"name": "get_weather", "description": "Weather", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"], "additionalProperties": false}}}],
		"tool_choice": "required",
		"parallel_tool_calls": false
	}`, llmconfig.Model{ID: "claude-haiku-5-5", Effort: true})

	if got["model"] != "claude-haiku-5-5" || got["max_tokens"] != 300.0 {
		t.Fatalf("model/max_tokens: %v %v", got["model"], got["max_tokens"])
	}
	if sys := got["system"].([]any)[0].(map[string]any)["text"]; sys != "Be brief." {
		t.Fatalf("system = %v", sys)
	}
	if effort := got["output_config"].(map[string]any)["effort"]; effort != "medium" {
		t.Fatalf("effort = %v", effort)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("want user, assistant, user turns; got %d: %v", len(msgs), msgs)
	}
	assistant := msgs[1].(map[string]any)["content"].([]any)
	if b := assistant[0].(map[string]any); b["type"] != "thinking" || b["signature"] != "sig-1" {
		t.Fatalf("thinking block must come first: %v", assistant)
	}
	if b := assistant[1].(map[string]any); b["type"] != "tool_use" || b["input"].(map[string]any)["city"] != "Lisbon" {
		t.Fatalf("tool_use: %v", b)
	}
	user := msgs[2].(map[string]any)["content"].([]any)
	if b := user[0].(map[string]any); b["type"] != "tool_result" || b["tool_use_id"] != "t1" {
		t.Fatalf("tool result must come first in the user turn: %v", user)
	}
	if b := user[1].(map[string]any); b["text"] != "And tomorrow?" {
		t.Fatalf("user text: %v", b)
	}
	tool := got["tools"].([]any)[0].(map[string]any)
	schema := tool["input_schema"].(map[string]any)
	if schema["additionalProperties"] != false || schema["required"].([]any)[0] != "city" {
		t.Fatalf("tool schema: %v", schema)
	}
	if tc := got["tool_choice"].(map[string]any); tc["type"] != "any" || tc["disable_parallel_tool_use"] != true {
		t.Fatalf("tool_choice: %v", tc)
	}
}

func TestAnthropicDefaultsAndPrefill(t *testing.T) {
	// No user turn first and an assistant turn last: both are fixed up. Effort
	// is dropped for models that don't support it.
	got := paramsJSON(t, `{"model": "m", "reasoning_effort": "high", "messages": [
		{"role": "developer", "content": "Rules."},
		{"role": "assistant", "content": "Hi"}
	]}`, llmconfig.Model{ID: "claude-haiku-4-5"})
	if got["max_tokens"] != 4096.0 {
		t.Fatalf("default max_tokens = %v", got["max_tokens"])
	}
	if _, ok := got["output_config"]; ok {
		t.Fatal("effort sent to a model without effort support")
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 || msgs[0].(map[string]any)["role"] != "user" || msgs[2].(map[string]any)["role"] != "user" {
		t.Fatalf("messages = %v", msgs)
	}
}

func TestAnthropicUnsupported(t *testing.T) {
	cfg := llmconfig.Default().Providers["claude-haiku"]
	for _, tt := range []struct{ req, want string }{
		{`{"model":"m","n":2,"messages":[{"role":"user","content":"x"}]}`, "n > 1"},
		{`{"model":"m","response_format":{"type":"json_object"},"messages":[{"role":"user","content":"x"}]}`, "response_format"},
		{`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`, "text only"},
		{`{"model":"m","messages":[{"role":"user","content":"x"}],"tools":[{"type":"function","function":{"name":"f"}}],"tool_choice":"sometimes"}`, "not supported"},
		{`{"model":"m","reasoning_effort":"extreme","messages":[{"role":"user","content":"x"}]}`, "reasoning_effort"},
	} {
		var r wire.ChatRequest
		_ = json.Unmarshal([]byte(tt.req), &r)
		_, werr := anthropicParams(Call{Model: llmconfig.Model{ID: "m", Effort: true}, Req: &r}, cfg)
		if werr == nil || werr.Status != 400 || !strings.Contains(werr.Message, tt.want) {
			t.Errorf("%s: want 400 containing %q, got %v", tt.req, tt.want, werr)
		}
	}
}

func TestDiscoveryFilters(t *testing.T) {
	ids := []string{"gpt-4.1", "gpt-test", "text-embedding-3", "llama-3-70b", "gpt-4.1-embedding", "gpt-4.1"}
	got := filterModels(ids, llmconfig.Discover{Include: []string{"gpt-*", "llama-*"}, Exclude: []string{"*embedding*"}})
	if strings.Join(got, ",") != "gpt-4.1,gpt-test,llama-3-70b" {
		t.Fatalf("filtered = %v", got)
	}
	merged := mergeModels([]llmconfig.Model{{ID: "gpt-4.1", Label: "GPT-4.1", Default: true}, {ID: "gone"}}, got)
	if len(merged) != 3 || merged[0].Label != "GPT-4.1" || !merged[0].Default {
		t.Fatalf("merged = %+v", merged)
	}
}
