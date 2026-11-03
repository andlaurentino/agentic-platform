package llms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"

	"agentic-platform/agent-assets/assets/core/llms/openaitest"
	"agentic-platform/agent-assets/llmconfig"
)

func gatewayProvider(t *testing.T, yaml string, discover bool) *Provider {
	t.Helper()
	cfg, err := llmconfig.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return NewProvider(context.Background(), cfg.Providers["gw"], discover)
}

func gatewayYAML(baseURL string, extra string) string {
	return `
providers:
  gw:
    name: Test Gateway
    type: openai
    baseURL: ` + baseURL + `
    apiKeyEnv: GW_KEY
    headers: { X-Team: "${TEAM_ID}" }
    models:
      - { id: gpt-test, label: GPT Test, default: true }
` + extra
}

func TestOpenAIRunnerTransferToolAndHeaders(t *testing.T) {
	srv := openaitest.New(nil,
		openaitest.Turn{ToolCalls: []openaitest.ToolCall{{ID: "call_0", Name: "transfer_to_agent", Args: map[string]any{"agent_name": "weather"}}}},
		openaitest.Turn{ToolCalls: []openaitest.ToolCall{{ID: "call_1", Name: "get_weather", Args: map[string]any{"city": "Lisbon"}}}},
		openaitest.Turn{Text: "It is sunny in Lisbon."},
	)
	defer srv.Close()
	t.Setenv("TEAM_ID", "payments")
	t.Setenv("OPENAI_BASE_URL", "http://must-not-be-used.invalid") // explicit config wins

	llm, err := gatewayProvider(t, gatewayYAML(srv.URL+"/v1", ""), false).New(context.Background(), nil, mapSecrets{"GW_KEY": "k-123"})
	if err != nil {
		t.Fatal(err)
	}
	type weatherArgs struct {
		City string `json:"city"`
	}
	weather, _ := functiontool.New(functiontool.Config{Name: "get_weather", Description: "Get the weather"},
		func(_ agent.ToolContext, a weatherArgs) (map[string]any, error) {
			return map[string]any{"city": a.City, "forecast": "sunny"}, nil
		})
	weatherAgent, _ := llmagent.New(llmagent.Config{Name: "weather", Description: "Weather", Model: llm, Tools: []tool.Tool{weather}})
	router, _ := llmagent.New(llmagent.Config{Name: "router", Description: "Router", Model: llm, SubAgents: []agent.Agent{weatherAgent}})
	r, _ := runner.New(runner.Config{AppName: "test", Agent: router, SessionService: session.InMemoryService(), AutoCreateSession: true})

	var partials int
	var transferred, calledTool bool
	var final strings.Builder
	for ev, err := range r.Run(context.Background(), "u", "s", genai.NewContentFromText("Weather in Lisbon?", genai.RoleUser), agent.RunConfig{StreamingMode: agent.StreamingModeSSE}) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Partial {
			partials++
			continue
		}
		transferred = transferred || ev.Actions.TransferToAgent == "weather"
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.FunctionCall != nil && p.FunctionCall.Name == "get_weather" && p.FunctionCall.Args["city"] == "Lisbon" {
				calledTool = true
			}
			if p.Text != "" && ev.Author == "weather" {
				final.WriteString(p.Text)
			}
		}
	}
	if !transferred || !calledTool || partials == 0 || final.String() != "It is sunny in Lisbon." {
		t.Fatalf("transferred=%v tool=%v partials=%d final=%q", transferred, calledTool, partials, final.String())
	}

	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	first, _ := json.Marshal(reqs[0])
	for _, want := range []string{`"model":"gpt-test"`, `"name":"transfer_to_agent"`, `"max_completion_tokens":4096`, `"include_usage":true`, `"role":"system"`} {
		if !strings.Contains(string(first), want) {
			t.Errorf("first request missing %s: %s", want, first)
		}
	}
	last, _ := json.Marshal(reqs[2]["messages"])
	for _, want := range []string{`"role":"tool"`, `"tool_call_id":"call_1"`, `"id":"call_1","type":"function"`} {
		if !strings.Contains(string(last), want) {
			t.Errorf("third request missing %s: %s", want, last)
		}
	}
	h := srv.Headers()[0]
	if h.Get("Authorization") != "Bearer k-123" || h.Get("X-Team") != "payments" {
		t.Errorf("headers: auth=%q team=%q", h.Get("Authorization"), h.Get("X-Team"))
	}
}

func TestOpenAIStreamUsageOffAndReasoningEffort(t *testing.T) {
	srv := openaitest.New(nil, openaitest.Turn{Text: "hi"})
	defer srv.Close()
	p := gatewayProvider(t, gatewayYAML(srv.URL+"/v1", "    streamUsage: false\n"), false)
	llm, err := p.New(context.Background(), json.RawMessage(`{"reasoningEffort":"low","maxTokens":100}`), mapSecrets{"GW_KEY": "k"})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range llm.GenerateContent(context.Background(), &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}, true) {
		if err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(srv.Requests()[0])
	if strings.Contains(string(b), "stream_options") || !strings.Contains(string(b), `"reasoning_effort":"low"`) || !strings.Contains(string(b), `"max_completion_tokens":100`) {
		t.Fatalf("request: %s", b)
	}
}

func TestProviderRejectsUnknownModel(t *testing.T) {
	p := gatewayProvider(t, gatewayYAML("http://localhost:1/v1", ""), false)
	if _, err := p.New(context.Background(), json.RawMessage(`{"model":"gpt-nope"}`), mapSecrets{"GW_KEY": "k"}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("got %v", err)
	}
}

func TestDiscoveryFiltersAndMerges(t *testing.T) {
	srv := openaitest.New([]string{"gpt-4.1", "gpt-test", "text-embedding-3", "llama-3-70b", "gpt-4.1-embedding"})
	defer srv.Close()
	p := gatewayProvider(t, gatewayYAML(srv.URL+"/v1", "    discover: { include: [\"gpt-*\"], exclude: [\"*embedding*\"] }\n"), true)

	var ids []string
	for _, m := range p.Models() {
		ids = append(ids, m.ID)
		if m.ID == "gpt-test" && (m.Label != "GPT Test" || !m.Default) {
			t.Errorf("configured label/default not kept: %+v", m)
		}
	}
	if got := strings.Join(ids, ","); got != "gpt-4.1,gpt-test" {
		t.Fatalf("models = %s", got)
	}
	if d := p.Discovery(); d.Status != "ok" || d.Count != 2 {
		t.Fatalf("discovery = %+v", d)
	}
	b, _ := json.Marshal(p.Manifest().ConfigSchema)
	if !strings.Contains(string(b), `"const":"gpt-4.1"`) || !strings.Contains(string(b), `"default":"gpt-test"`) {
		t.Fatalf("schema: %s", b)
	}
}

func TestDiscoveryFailureFallsBack(t *testing.T) {
	srv := openaitest.New(nil)
	url := srv.URL
	srv.Close() // unreachable
	p := gatewayProvider(t, gatewayYAML(url+"/v1", "    discover: {}\n"), true)
	if d := p.Discovery(); d.Status != "failed" || d.Error == "" {
		t.Fatalf("discovery = %+v", d)
	}
	if len(p.Models()) != 1 || p.Models()[0].ID != "gpt-test" {
		t.Fatalf("models = %+v", p.Models())
	}
}

func TestManifestExtras(t *testing.T) {
	t.Setenv("GW_KEY", "")
	m := gatewayProvider(t, gatewayYAML("https://user:pass@gw.internal/v1?token=x", ""), false).Manifest()
	if m.ID != "core.llm.gw" || m.Extra["credentialsConfigured"] != false || m.Extra["baseURL"] != "https://gw.internal/v1" {
		t.Fatalf("manifest = %+v", m.Extra)
	}
	if len(m.Secrets) != 1 || m.Secrets[0].Name != "GW_KEY" {
		t.Fatalf("secrets = %+v", m.Secrets)
	}
}
