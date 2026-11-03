package llms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"

	"agentic-platform/agent-assets/assets/core/llms/claudetest"
	"agentic-platform/agent-assets/llmconfig"
)

type mapSecrets map[string]string

func (m mapSecrets) Get(name string) (string, error) { return m[name], nil }

// haikuProvider is the embedded claude-haiku provider pointed at baseURL.
func haikuProvider(t *testing.T, baseURL string) *Provider {
	t.Helper()
	cfg := llmconfig.Default().Providers["claude-haiku"]
	cfg.BaseURL = baseURL
	return NewProvider(context.Background(), cfg, false)
}

func roles(t *testing.T, msgs []anthropic.MessageParam) []string {
	t.Helper()
	var out []string
	for _, m := range msgs {
		b, _ := json.Marshal(m)
		var v struct {
			Role string `json:"role"`
		}
		_ = json.Unmarshal(b, &v)
		out = append(out, v.Role)
	}
	return out
}

func TestContentsToMessages(t *testing.T) {
	contents := []*genai.Content{
		genai.NewContentFromText("hi", genai.RoleUser),
		{Role: genai.RoleModel, Parts: []*genai.Part{
			{Thought: true, Text: "", ThoughtSignature: []byte("sig-1")},
			{FunctionCall: &genai.FunctionCall{ID: "t1", Name: "get_weather", Args: map[string]any{"city": "Lisbon"}}},
		}},
		{Role: genai.RoleUser, Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{ID: "t1", Name: "get_weather", Response: map[string]any{"forecast": "sunny"}}},
		}},
		genai.NewContentFromText("thanks", genai.RoleUser),
		genai.NewContentFromText("you're welcome", genai.RoleModel),
	}
	msgs, err := contentsToMessages(contents)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(roles(t, msgs), ","), "user,assistant,user,assistant,user"; got != want {
		t.Fatalf("roles = %s, want %s", got, want)
	}
	b, _ := json.Marshal(msgs)
	body := string(b)
	for _, want := range []string{`"signature":"sig-1"`, `"type":"tool_use"`, `"tool_use_id":"t1"`, `Continue processing`} {
		if !strings.Contains(body, want) {
			t.Errorf("messages missing %s: %s", want, body)
		}
	}
	// The merged user turn must start with the tool result, followed by the text.
	if strings.Index(body, `"tool_use_id":"t1"`) > strings.Index(body, `"thanks"`) {
		t.Errorf("tool_result must come before text in the user turn: %s", body)
	}
}

func TestContentsToMessagesStartsWithUser(t *testing.T) {
	msgs, err := contentsToMessages([]*genai.Content{genai.NewContentFromText("hello", genai.RoleModel)})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(roles(t, msgs), ","); got != "user,assistant,user" {
		t.Fatalf("roles = %s", got)
	}
}

func TestGenaiSchemaToJSON(t *testing.T) {
	s := genaiSchemaToJSON(&genai.Schema{
		Type:       genai.TypeObject,
		Properties: map[string]*genai.Schema{"request": {Type: genai.TypeString, Description: "the task"}},
		Required:   []string{"request"},
	})
	b, _ := json.Marshal(s)
	if got := string(b); !strings.Contains(got, `"type":"object"`) || !strings.Contains(got, `"type":"string"`) {
		t.Fatalf("unexpected schema %s", got)
	}
}

func TestRunnerTransferToolAndThinkingRoundTrip(t *testing.T) {
	srv := claudetest.New(
		claudetest.Turn{Blocks: []claudetest.Block{claudetest.ToolUse("t0", "transfer_to_agent", map[string]any{"agent_name": "weather"})}},
		claudetest.Turn{Blocks: []claudetest.Block{
			claudetest.Thinking("need the forecast", "sig-1"),
			claudetest.ToolUse("t1", "get_weather", map[string]any{"city": "Lisbon"}),
		}},
		claudetest.Turn{Blocks: []claudetest.Block{claudetest.Text("It is sunny in Lisbon.")}},
	)
	defer srv.Close()

	llm, err := haikuProvider(t, srv.URL).New(context.Background(), nil, mapSecrets{"ANTHROPIC_API_KEY": "test"})
	if err != nil {
		t.Fatal(err)
	}
	type weatherArgs struct {
		City string `json:"city"`
	}
	weather, err := functiontool.New(functiontool.Config{Name: "get_weather", Description: "Get the weather for a city"},
		func(_ agent.ToolContext, a weatherArgs) (map[string]any, error) {
			return map[string]any{"city": a.City, "forecast": "sunny"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	weatherAgent, err := llmagent.New(llmagent.Config{Name: "weather", Description: "Weather", Model: llm, Tools: []tool.Tool{weather}})
	if err != nil {
		t.Fatal(err)
	}
	router, err := llmagent.New(llmagent.Config{Name: "router", Description: "Router", Model: llm, SubAgents: []agent.Agent{weatherAgent}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(runner.Config{AppName: "test", Agent: router, SessionService: session.InMemoryService(), AutoCreateSession: true})
	if err != nil {
		t.Fatal(err)
	}

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
			if p.FunctionCall != nil && p.FunctionCall.Name == "get_weather" {
				calledTool = true
			}
			if p.Text != "" && !p.Thought && ev.Author == "weather" {
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
	if !strings.Contains(string(first), `"name":"transfer_to_agent"`) || strings.Contains(string(first), "temperature") {
		t.Errorf("first request: want transfer tool and no temperature: %s", first)
	}
	if !strings.Contains(string(first), `"effort":"low"`) {
		t.Errorf("first request should set effort for haiku 5.5: %s", first)
	}
	last, _ := json.Marshal(reqs[2]["messages"])
	for _, want := range []string{`"signature":"sig-1"`, `"tool_use_id":"t1"`} {
		if !strings.Contains(string(last), want) {
			t.Errorf("third request missing %s: %s", want, last)
		}
	}
}
