package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"agentic-platform/ms-llm-gateway/internal/llmconfig"
	"agentic-platform/ms-llm-gateway/internal/providers"
	"agentic-platform/ms-llm-gateway/internal/providers/claudetest"
	"agentic-platform/ms-llm-gateway/internal/providers/openaitest"
)

// gateway starts the gateway in front of a fake Claude and a fake OpenAI-compatible upstream.
func gateway(t *testing.T, claude *claudetest.Server, upstream *openaitest.Server, keys ...string) *httptest.Server {
	t.Helper()
	t.Setenv("UP_KEY", "up-secret")
	t.Setenv("TEAM_ID", "payments")
	cfg, err := llmconfig.Parse([]byte(`
defaults: { model: claude/claude-haiku-5-5 }
providers:
  claude:
    name: Claude
    type: anthropic
    baseURL: ` + claude.URL + `
    auth: none
    models:
      - { id: claude-haiku-5-5, label: Claude Haiku 5.5, effort: true }
  gw:
    name: Upstream
    type: openai
    baseURL: ` + upstream.URL + `/v1
    apiKeyEnv: UP_KEY
    headers: { X-Team: "${TEAM_ID}" }
    discover: { include: ["gpt-*"] }
  keyless:
    type: anthropic
    apiKeyEnv: GATEWAY_TEST_MISSING_KEY
    models: [{ id: m }]
`))
	if err != nil {
		t.Fatal(err)
	}
	set := providers.NewSet(cfg)
	set.DiscoverAll(context.Background())
	srv := httptest.NewServer((&Server{Providers: set, APIKeys: keys}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func client(srv *httptest.Server, key string) openai.Client {
	return openai.NewClient(option.WithBaseURL(srv.URL+"/v1"), option.WithAPIKey(key), option.WithMaxRetries(0))
}

var weatherTool = openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
	Name:       "get_weather",
	Parameters: openai.FunctionParameters{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"}},
})

// A full tool loop through the official OpenAI client: Claude thinks, calls a
// tool, and the signed thinking block survives the round-trip.
func TestClaudeToolLoopWithThinking(t *testing.T) {
	claude := claudetest.New(
		claudetest.Turn{Blocks: []claudetest.Block{
			claudetest.Thinking("need the forecast", "sig-1"),
			claudetest.ToolUse("t1", "get_weather", map[string]any{"city": "Lisbon"}),
		}},
		claudetest.Turn{Blocks: []claudetest.Block{claudetest.Text("It is sunny in Lisbon.")}},
	)
	defer claude.Close()
	up := openaitest.New(nil)
	defer up.Close()
	c := client(gateway(t, claude, up), "")
	ctx := context.Background()

	params := openai.ChatCompletionNewParams{
		Model:           "claude/claude-haiku-5-5",
		Messages:        []openai.ChatCompletionMessageParamUnion{openai.SystemMessage("Be brief."), openai.UserMessage("Weather in Lisbon?")},
		Tools:           []openai.ChatCompletionToolUnionParam{weatherTool},
		ReasoningEffort: "high",
		StreamOptions:   openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)},
	}
	stream := c.Chat.Completions.NewStreaming(ctx, params)
	acc := openai.ChatCompletionAccumulator{}
	var reasoning string
	var blocks []map[string]any
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		if len(chunk.Choices) == 0 {
			continue
		}
		extra := chunk.Choices[0].Delta.JSON.ExtraFields
		if f, ok := extra["reasoning_content"]; ok {
			var s string
			_ = json.Unmarshal([]byte(f.Raw()), &s)
			reasoning += s
		}
		if f, ok := extra["thinking_blocks"]; ok {
			var bs []map[string]any
			_ = json.Unmarshal([]byte(f.Raw()), &bs)
			blocks = append(blocks, bs...)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	msg := acc.Choices[0].Message
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "t1" || msg.ToolCalls[0].Function.Arguments != `{"city":"Lisbon"}` {
		t.Fatalf("tool calls = %+v", msg.ToolCalls)
	}
	if acc.Choices[0].FinishReason != "tool_calls" || acc.Usage.CompletionTokens != 5 {
		t.Fatalf("finish = %s usage = %+v", acc.Choices[0].FinishReason, acc.Usage)
	}
	if reasoning != "need the forecast" || len(blocks) != 1 || blocks[0]["signature"] != "sig-1" {
		t.Fatalf("reasoning = %q blocks = %v", reasoning, blocks)
	}
	if effort := claude.Requests()[0]["output_config"].(map[string]any)["effort"]; effort != "high" {
		t.Fatalf("effort = %v", effort)
	}

	assistant := msg.ToParam()
	assistant.OfAssistant.SetExtraFields(map[string]any{"thinking_blocks": blocks})
	params.Messages = append(params.Messages, assistant, openai.ToolMessage(`{"temp":21}`, "t1"))
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{}
	resp, err := c.Chat.Completions.New(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.Content != "It is sunny in Lisbon." || resp.Model != "claude/claude-haiku-5-5" {
		t.Fatalf("response = %+v", resp)
	}

	second := claude.Requests()[1]["messages"].([]any)
	turn := second[1].(map[string]any)["content"].([]any)
	if b := turn[0].(map[string]any); b["type"] != "thinking" || b["signature"] != "sig-1" || b["thinking"] != "need the forecast" {
		t.Fatalf("thinking block not sent back first: %v", turn)
	}
	if b := second[2].(map[string]any)["content"].([]any)[0].(map[string]any); b["type"] != "tool_result" || b["tool_use_id"] != "t1" {
		t.Fatalf("tool result: %v", b)
	}
}

func TestOpenAIPassthrough(t *testing.T) {
	claude := claudetest.New()
	defer claude.Close()
	up := openaitest.New([]string{"gpt-4.1", "gpt-test", "text-embedding-3"},
		openaitest.Turn{Text: "hi"},
		openaitest.Turn{ToolCalls: []openaitest.ToolCall{{ID: "call_1", Name: "get_weather", Args: map[string]any{"city": "Porto"}}}},
	)
	defer up.Close()
	c := client(gateway(t, claude, up), "")
	ctx := context.Background()

	assistant := openai.ChatCompletionAssistantMessageParam{}
	assistant.Content.OfString = openai.String("earlier")
	assistant.SetExtraFields(map[string]any{"thinking_blocks": []any{map[string]any{"type": "thinking", "signature": "s"}}, "reasoning_content": "x"})
	resp, err := c.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:           "gw/gpt-4.1",
		Messages:        []openai.ChatCompletionMessageParamUnion{openai.UserMessage("a"), {OfAssistant: &assistant}, openai.UserMessage("b")},
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.Content != "hi" {
		t.Fatalf("content = %q", resp.Choices[0].Message.Content)
	}
	req := up.Requests()[0]
	if req["model"] != "gpt-4.1" || req["reasoning_effort"] != nil {
		t.Fatalf("upstream request = %v", req)
	}
	if m := req["messages"].([]any)[1].(map[string]any); m["thinking_blocks"] != nil || m["reasoning_content"] != nil {
		t.Fatalf("reasoning extension forwarded: %v", m)
	}
	if h := up.Headers()[0]; h.Get("Authorization") != "Bearer up-secret" || h.Get("X-Team") != "payments" {
		t.Fatalf("headers = %v", h)
	}

	stream := c.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    "gw/gpt-test",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("weather?")},
		Tools:    []openai.ChatCompletionToolUnionParam{weatherTool},
	})
	acc := openai.ChatCompletionAccumulator{}
	for stream.Next() {
		acc.AddChunk(stream.Current())
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if tc := acc.Choices[0].Message.ToolCalls; len(tc) != 1 || tc[0].Function.Arguments != `{"city":"Porto"}` {
		t.Fatalf("streamed tool calls = %+v", tc)
	}
}

func TestModelsAndErrors(t *testing.T) {
	claude := claudetest.New() // no scripted turns: every call is a 400 from "Anthropic"
	defer claude.Close()
	up := openaitest.New([]string{"gpt-4.1", "text-embedding-3"})
	defer up.Close()
	t.Setenv("GATEWAY_TEST_MISSING_KEY", "")
	srv := gateway(t, claude, up)
	c := client(srv, "")
	ctx := context.Background()

	page, err := c.Models.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range page.Data {
		ids = append(ids, m.ID)
		if m.ID == "claude/claude-haiku-5-5" {
			if m.JSON.ExtraFields["display_name"].Raw() != `"Claude · Claude Haiku 5.5"` || m.JSON.ExtraFields["default"].Raw() != "true" {
				t.Fatalf("extensions = %v", m.JSON.ExtraFields)
			}
		}
	}
	if strings.Join(ids, ",") != "claude/claude-haiku-5-5,gw/gpt-4.1,keyless/m" {
		t.Fatalf("models = %v", ids)
	}

	call := func(model string) *openai.Error {
		_, err := c.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{Model: model, Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("x")}})
		var apiErr *openai.Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("%s: want API error, got %v", model, err)
		}
		return apiErr
	}
	if e := call("gw/nope"); e.StatusCode != 404 || e.Code != "model_not_found" {
		t.Fatalf("unknown model: %d %s", e.StatusCode, e.Code)
	}
	if e := call("keyless/m"); e.StatusCode != http.StatusBadGateway || !strings.Contains(e.Message, "GATEWAY_TEST_MISSING_KEY is not set") {
		t.Fatalf("missing key: %d %s", e.StatusCode, e.Message)
	}
	if e := call("claude-haiku-5-5"); e.StatusCode != 400 || !strings.Contains(e.Message, "no scripted turns left") {
		t.Fatalf("bare id should resolve and surface the upstream 400: %d %s", e.StatusCode, e.Message)
	}

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var health struct {
		Providers []ProviderHealth `json:"providers"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&health)
	if len(health.Providers) != 3 || health.Providers[1].Discovery.Status != "ok" || health.Providers[2].CredentialsConfigured {
		t.Fatalf("health = %+v", health)
	}
}

func TestInboundAuth(t *testing.T) {
	claude := claudetest.New()
	defer claude.Close()
	up := openaitest.New(nil)
	defer up.Close()
	srv := gateway(t, claude, up, "k1", "k2")

	bad := client(srv, "wrong")
	_, err := bad.Models.List(context.Background())
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Fatalf("want 401, got %v", err)
	}
	good := client(srv, "k2")
	if _, err := good.Models.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resp, err := http.Get(srv.URL + "/healthz"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz must stay open: %v %v", resp, err)
	}
}
