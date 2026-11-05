//go:build live

package server

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"agentic-platform/ms-llm-gateway/internal/llmconfig"
	"agentic-platform/ms-llm-gateway/internal/providers"
)

// TestLiveClaude calls the real Anthropic API through the gateway with the
// embedded llms.yaml: go test -tags live ./internal/server (needs ANTHROPIC_API_KEY with credits).
func TestLiveClaude(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	srv := httptest.NewServer((&Server{Providers: providers.NewSet(llmconfig.Default())}).Handler())
	defer srv.Close()
	c := openai.NewClient(option.WithBaseURL(srv.URL+"/v1"), option.WithAPIKey(""))
	ctx := context.Background()

	params := openai.ChatCompletionNewParams{
		Model:    "claude-haiku/claude-haiku-5-5",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("What is the weather in Lisbon? Use the tool.")},
		Tools:    []openai.ChatCompletionToolUnionParam{weatherTool},
	}
	stream := c.Chat.Completions.NewStreaming(ctx, params)
	acc := openai.ChatCompletionAccumulator{}
	for stream.Next() {
		acc.AddChunk(stream.Current())
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	msg := acc.Choices[0].Message
	if len(msg.ToolCalls) == 0 {
		t.Fatalf("expected a tool call, got %q", msg.Content)
	}
	params.Messages = append(params.Messages, msg.ToParam(), openai.ToolMessage(`{"forecast":"sunny","tempC":24}`, msg.ToolCalls[0].ID))
	resp, err := c.Chat.Completions.New(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("answer: %s (%d tokens)", resp.Choices[0].Message.Content, resp.Usage.TotalTokens)
	if resp.Choices[0].Message.Content == "" {
		t.Fatal("empty answer")
	}
}
