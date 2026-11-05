package llmproviders

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

	"agentic-platform/lib-agent-assets/assets"
	"agentic-platform/ms-ai-platform/internal/llmconfig"
	"agentic-platform/ms-ai-platform/internal/llmproviders/gatewaytest"
	"agentic-platform/ms-ai-platform/internal/secrets"
)

func provider(t *testing.T, baseURL, extra string) *Provider {
	t.Helper()
	cfg, err := llmconfig.Parse([]byte(`
providers:
  gw:
    name: Test Gateway
    baseURL: ` + baseURL + `
    apiKeyEnv: GW_KEY
    headers: { X-Team: "${TEAM_ID}" }
` + extra))
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg.Providers["gw"])
}

var gatewayModels = []gatewaytest.Model{
	{ID: "claude-haiku/claude-haiku-5-5", DisplayName: "Claude Haiku · Claude Haiku 5.5", Default: true},
	{ID: "gw/gpt-test", DisplayName: "Upstream · GPT Test"},
	{ID: "gw/text-embedding-3"},
}

// A Claude agent behind the gateway: hand-off, a tool call made after thinking,
// and the signed thinking block sent back on the next request.
func TestRunnerTransferToolAndThinking(t *testing.T) {
	srv := gatewaytest.New(gatewayModels,
		gatewaytest.Turn{ToolCalls: []gatewaytest.ToolCall{{ID: "call_0", Name: "transfer_to_agent", Args: map[string]any{"agent_name": "weather"}}}},
		gatewaytest.Turn{
			ThinkingBlocks: []map[string]any{{"type": "thinking", "thinking": "need the forecast", "signature": "sig-1"}},
			ToolCalls:      []gatewaytest.ToolCall{{ID: "call_1", Name: "get_weather", Args: map[string]any{"city": "Lisbon"}}},
		},
		gatewaytest.Turn{Text: "It is sunny in Lisbon."},
	)
	defer srv.Close()
	t.Setenv("TEAM_ID", "payments")
	t.Setenv("GW_KEY", "k-123")
	t.Setenv("OPENAI_BASE_URL", "http://must-not-be-used.invalid") // explicit config wins

	p := provider(t, srv.URL+"/v1", "")
	p.Refresh(context.Background())
	llm, err := p.New(context.Background(), nil, secrets.Env{})
	if err != nil {
		t.Fatal(err)
	}
	if llm.Name() != "claude-haiku/claude-haiku-5-5" {
		t.Fatalf("default model = %s", llm.Name())
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
	var transferred, calledTool, thought bool
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
		for _, part := range ev.Content.Parts {
			thought = thought || (part.Thought && part.Text == "need the forecast")
			if part.FunctionCall != nil && part.FunctionCall.Name == "get_weather" && part.FunctionCall.Args["city"] == "Lisbon" {
				calledTool = true
			}
			if part.Text != "" && !part.Thought && ev.Author == "weather" {
				final.WriteString(part.Text)
			}
		}
	}
	if !transferred || !calledTool || !thought || partials == 0 || final.String() != "It is sunny in Lisbon." {
		t.Fatalf("transferred=%v tool=%v thought=%v partials=%d final=%q", transferred, calledTool, thought, partials, final.String())
	}

	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	first, _ := json.Marshal(reqs[0])
	for _, want := range []string{`"model":"claude-haiku/claude-haiku-5-5"`, `"name":"transfer_to_agent"`, `"max_completion_tokens":4096`, `"include_usage":true`, `"role":"system"`} {
		if !strings.Contains(string(first), want) {
			t.Errorf("first request missing %s: %s", want, first)
		}
	}
	if _, ok := reqs[0]["reasoning_effort"]; ok {
		t.Error("reasoning_effort sent without being configured")
	}
	last, _ := json.Marshal(reqs[2]["messages"])
	for _, want := range []string{`"role":"tool"`, `"tool_call_id":"call_1"`, `"thinking_blocks":[{"signature":"sig-1","thinking":"need the forecast","type":"thinking"}]`} {
		if !strings.Contains(string(last), want) {
			t.Errorf("third request missing %s: %s", want, last)
		}
	}
	h := srv.Headers()[0]
	if h.Get("Authorization") != "Bearer k-123" || h.Get("X-Team") != "payments" {
		t.Errorf("headers: auth=%q team=%q", h.Get("Authorization"), h.Get("X-Team"))
	}
}

func TestEffortAndStreamUsageOff(t *testing.T) {
	srv := gatewaytest.New(gatewayModels, gatewaytest.Turn{Text: "hi"})
	defer srv.Close()
	p := provider(t, srv.URL+"/v1", "    streamUsage: false\n    auth: optional\n")
	t.Setenv("GW_KEY", "")
	p.Refresh(context.Background())
	llm, err := p.New(context.Background(), json.RawMessage(`{"model":"gw/gpt-test","effort":"low","maxTokens":100}`), secrets.Env{})
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
	if h := srv.Headers()[0].Get("Authorization"); h != "" {
		t.Fatalf("optional key unset, but Authorization = %q", h)
	}
}

func TestRefreshFiltersLabelsAndDefaults(t *testing.T) {
	srv := gatewaytest.New(gatewayModels)
	defer srv.Close()
	p := provider(t, srv.URL+"/v1", `    exclude: ["*embedding*"]
    models: [{ id: gw/gpt-test, label: GPT (team), default: true }]
`)
	if st := p.Status(); st.State != "pending" || len(p.Models()) != 1 {
		t.Fatalf("before refresh: %+v %+v", st, p.Models())
	}
	if !p.Refresh(context.Background()) {
		t.Fatal("first refresh should report a change")
	}
	if p.Refresh(context.Background()) {
		t.Fatal("unchanged list reported as a change")
	}
	models := p.Models()
	if len(models) != 2 || models[0].Label != "Claude Haiku · Claude Haiku 5.5" || models[1].Label != "GPT (team)" || !models[1].Default || models[0].Default {
		t.Fatalf("models = %+v", models)
	}
	b, _ := json.Marshal(p.Manifest().ConfigSchema)
	if !strings.Contains(string(b), `"const":"claude-haiku/claude-haiku-5-5"`) || !strings.Contains(string(b), `"default":"gw/gpt-test"`) || strings.Contains(string(b), "embedding") {
		t.Fatalf("schema: %s", b)
	}
	if _, err := p.New(context.Background(), json.RawMessage(`{"model":"gw/nope"}`), secrets.Env{}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("unknown model: %v", err)
	}

	// The gateway goes away: the last list is kept and the status says why.
	srv.Close()
	if !p.Refresh(context.Background()) || p.Status().State != "failed" || len(p.Models()) != 2 {
		t.Fatalf("after failure: %+v %+v", p.Status(), p.Models())
	}
}

func TestUnavailableProvider(t *testing.T) {
	srv := gatewaytest.New(nil)
	url := srv.URL
	srv.Close()
	p := provider(t, url+"/v1", "    auth: none\n")
	if _, err := p.New(context.Background(), nil, secrets.Env{}); err == nil || !strings.Contains(err.Error(), "has not been reached yet") {
		t.Fatalf("pending: %v", err)
	}
	p.Refresh(context.Background())
	if _, err := p.New(context.Background(), nil, secrets.Env{}); err == nil || !strings.Contains(err.Error(), "Test Gateway is unavailable") {
		t.Fatalf("failed: %v", err)
	}
}

func TestManifestAndDefaultModel(t *testing.T) {
	t.Setenv("GW_KEY", "")
	m := provider(t, "https://user:pass@gw.internal/v1?token=x", "").Manifest()
	if m.ID != "core.llm.gw" || m.Extra["credentialsConfigured"] != false || m.Extra["baseURL"] != "https://gw.internal/v1" || m.Extra["apiKeyEnv"] != "GW_KEY" {
		t.Fatalf("manifest = %+v", m.Extra)
	}

	cfg, err := llmconfig.Parse([]byte(`
providers:
  a: { baseURL: "http://a/v1", auth: none, models: [{ id: x }, { id: y, default: true }] }
  b: { baseURL: "http://b/v1", auth: none, models: [{ id: z }] }
`))
	if err != nil {
		t.Fatal(err)
	}
	c := assets.New()
	Register(c, cfg)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.DefaultModel() != "a:y" {
		t.Fatalf("default = %s", c.DefaultModel())
	}
	cfg.Defaults.GuardrailModel = "b:z"
	c = assets.New()
	Register(c, cfg)
	if c.DefaultModel() != "b:z" {
		t.Fatalf("configured default = %s", c.DefaultModel())
	}
}
