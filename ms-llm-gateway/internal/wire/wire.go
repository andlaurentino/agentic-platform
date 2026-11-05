// Package wire holds the OpenAI Chat Completions JSON the gateway speaks to
// its clients, plus the reasoning extension (reasoning_content and
// thinking_blocks, as LiteLLM uses them) that lets Claude's thinking survive
// a tool loop.
package wire

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChatRequest is a POST /v1/chat/completions body. Fields the gateway does not
// use are ignored; OpenAI-compatible upstreams receive the original body.
type ChatRequest struct {
	Model               string          `json:"model"`
	Messages            []Message       `json:"messages"`
	Tools               []Tool          `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
	MaxTokens           *int64          `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int64          `json:"max_completion_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	Stop                json.RawMessage `json:"stop,omitempty"`
	ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	StreamOptions       *StreamOptions  `json:"stream_options,omitempty"`
	N                   *int            `json:"n,omitempty"`
	ResponseFormat      json.RawMessage `json:"response_format,omitempty"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// IncludeUsage reports whether the client asked for a usage chunk.
func (r *ChatRequest) IncludeUsage() bool {
	return r.StreamOptions != nil && r.StreamOptions.IncludeUsage
}

// OutputLimit returns max_completion_tokens, then max_tokens, then 0.
func (r *ChatRequest) OutputLimit() int64 {
	switch {
	case r.MaxCompletionTokens != nil:
		return *r.MaxCompletionTokens
	case r.MaxTokens != nil:
		return *r.MaxTokens
	}
	return 0
}

// StopSequences decodes "stop" (a string or a list of strings).
func (r *ChatRequest) StopSequences() ([]string, error) {
	if len(r.Stop) == 0 || string(r.Stop) == "null" {
		return nil, nil
	}
	var one string
	if json.Unmarshal(r.Stop, &one) == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(r.Stop, &many); err != nil {
		return nil, fmt.Errorf("stop must be a string or a list of strings")
	}
	return many, nil
}

// Message is one chat message in a request.
type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	// Reasoning extension: the thinking an assistant turn produced, sent back unchanged.
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ThinkingBlocks   []ThinkingBlock `json:"thinking_blocks,omitempty"`
}

// Text returns the message content as plain text. Content may be a string, null,
// or a list of {type:"text"} parts; other part types are not supported.
func (m Message) Text() (string, error) {
	if len(m.Content) == 0 || string(m.Content) == "null" {
		return "", nil
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return "", fmt.Errorf("%s message: content must be a string or a list of parts", m.Role)
	}
	var texts []string
	for _, p := range parts {
		if p.Type != "text" {
			return "", fmt.Errorf("%s message: content part type %q is not supported (text only)", m.Role, p.Type)
		}
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, "\n\n"), nil
}

// ThinkingBlock is a signed Claude thinking block, or a redacted one.
type ThinkingBlock struct {
	Type      string `json:"type"` // thinking | redacted_thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
}

type ToolCall struct {
	Index    *int         `json:"index,omitempty"` // stream deltas only
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ChatCompletion is a non-streaming response.
type ChatCompletion struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"` // chat.completion
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type ResponseMessage struct {
	Role             string          `json:"role"`
	Content          *string         `json:"content"`
	ToolCalls        []ToolCall      `json:"tool_calls,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ThinkingBlocks   []ThinkingBlock `json:"thinking_blocks,omitempty"`
}

// Chunk is one streaming event.
type Chunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"` // chat.completion.chunk
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

type ChunkChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type Delta struct {
	Role             string          `json:"role,omitempty"`
	Content          *string         `json:"content,omitempty"`
	ToolCalls        []ToolCall      `json:"tool_calls,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ThinkingBlocks   []ThinkingBlock `json:"thinking_blocks,omitempty"`
}

type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// ModelList is the GET /v1/models response.
type ModelList struct {
	Object string  `json:"object"` // list
	Data   []Model `json:"data"`
}

// Model is one entry of GET /v1/models. DisplayName and Default are gateway
// extensions; standard clients ignore them.
type Model struct {
	ID          string `json:"id"`
	Object      string `json:"object"` // model
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	DisplayName string `json:"display_name,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }
