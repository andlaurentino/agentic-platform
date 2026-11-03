//go:build live

// Live tests call the real Anthropic API. Run with:
//
//	ANTHROPIC_API_KEY=... go test -tags live ./assets/core/llms/
package llms

import (
	"context"
	"os"
	"strings"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"

	"agentic-platform/agent-assets/llmconfig"
)

type envSecrets struct{}

func (envSecrets) Get(name string) (string, error) { return os.Getenv(name), nil }

func TestLiveAgentWithToolStreaming(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	ctx := context.Background()
	llm, err := NewProvider(ctx, llmconfig.Default().Providers["claude-haiku"], false).New(ctx, nil, envSecrets{})
	if err != nil {
		t.Fatal(err)
	}

	type weatherArgs struct {
		City string `json:"city"`
	}
	weather, err := functiontool.New(functiontool.Config{Name: "get_weather", Description: "Get the weather for a city"},
		func(_ agent.ToolContext, a weatherArgs) (map[string]any, error) {
			return map[string]any{"city": a.City, "forecast": "sunny", "celsius": 23}, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	weatherAgent, err := llmagent.New(llmagent.Config{
		Name: "weather", Description: "Answers weather questions", Model: llm,
		Instruction: "Use get_weather to answer. Reply in one sentence.",
		Tools:       []tool.Tool{weather},
	})
	if err != nil {
		t.Fatal(err)
	}
	router, err := llmagent.New(llmagent.Config{
		Name: "router", Description: "Routes requests", Model: llm,
		Instruction: "Transfer weather questions to the weather agent.",
		SubAgents:   []agent.Agent{weatherAgent},
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := runner.New(runner.Config{AppName: "spike", Agent: router, SessionService: session.InMemoryService(), AutoCreateSession: true})
	if err != nil {
		t.Fatal(err)
	}

	var partials int
	var final strings.Builder
	var calledTool, transferred bool
	for ev, err := range r.Run(ctx, "u1", "s1", genai.NewContentFromText("What's the weather in Lisbon?", genai.RoleUser), agent.RunConfig{StreamingMode: agent.StreamingModeSSE}) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Partial {
			partials++
			continue
		}
		if ev.Actions.TransferToAgent != "" {
			transferred = true
		}
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
	t.Logf("partials=%d transferred=%v tool=%v final=%q", partials, transferred, calledTool, final.String())
	if !transferred || !calledTool || final.Len() == 0 || partials == 0 {
		t.Fatalf("expected transfer, tool call, streamed final text")
	}

	// Second turn on the same session: history now contains thinking blocks and tool results.
	for ev, err := range r.Run(ctx, "u1", "s1", genai.NewContentFromText("And in Porto?", genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			t.Fatalf("second turn: %v", err)
		}
		_ = ev
	}
}
