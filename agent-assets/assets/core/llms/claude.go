// Package llms holds LLM connector assets.
package llms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"agentic-platform/agent-assets/sdk"
)

const (
	ClaudeHaikuID = "core.llm.claude-haiku"
	// APIKeySecret is read from the environment by the platform.
	APIKeySecret = "ANTHROPIC_API_KEY"

	ModelHaiku55 = "claude-haiku-5-5"
	ModelHaiku45 = "claude-haiku-4-5"

	redactedPrefix = "redacted:"
)

// ClaudeConfig is what builders set on an agent that uses Claude Haiku.
type ClaudeConfig struct {
	Model     string `json:"model,omitempty" jsonschema:"title=Model,enum=claude-haiku-5-5,enum=claude-haiku-4-5,default=claude-haiku-5-5"`
	MaxTokens int64  `json:"maxTokens,omitempty" jsonschema:"title=Max output tokens,minimum=256,maximum=64000,default=4096"`
	Effort    string `json:"effort,omitempty" jsonschema:"title=Effort,enum=low,enum=medium,enum=high,default=low" jsonschema_description:"How much the model thinks before answering. Only used by claude-haiku-5-5."`
}

var defaultClaudeConfig = ClaudeConfig{Model: ModelHaiku55, MaxTokens: 4096, Effort: "low"}

// ClaudeHaiku is the Claude Haiku connector asset.
type ClaudeHaiku struct{}

func (ClaudeHaiku) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:           ClaudeHaikuID,
		Kind:         sdk.KindLLM,
		Domain:       "core",
		Name:         "Claude Haiku",
		Description:  "Anthropic's fastest, lowest-cost Claude models. Good for routing, chat and tool use.",
		Owner:        "@ai-platform",
		Tags:         []string{"anthropic", "claude"},
		Lifecycle:    sdk.LifecycleStable,
		ConfigSchema: sdk.SchemaFor[ClaudeConfig](),
		Secrets:      []sdk.SecretRef{{Name: APIKeySecret, Description: "Anthropic API key"}},
	}
}

func (ClaudeHaiku) New(_ context.Context, raw json.RawMessage, s sdk.Secrets) (model.LLM, error) {
	cfg, err := sdk.DecodeConfig(raw, defaultClaudeConfig)
	if err != nil {
		return nil, err
	}
	key, err := s.Get(APIKeySecret)
	if err != nil {
		return nil, err
	}
	return NewClaudeModel(anthropic.NewClient(option.WithAPIKey(key)), cfg)
}

// NewClaudeModel wraps an Anthropic client as an ADK model. Exposed for tests.
func NewClaudeModel(client anthropic.Client, cfg ClaudeConfig) (model.LLM, error) {
	if cfg.Model != ModelHaiku55 && cfg.Model != ModelHaiku45 {
		return nil, fmt.Errorf("unsupported model %q", cfg.Model)
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = defaultClaudeConfig.MaxTokens
	}
	return &claudeModel{client: client, cfg: cfg}, nil
}

type claudeModel struct {
	client anthropic.Client
	cfg    ClaudeConfig
}

func (m *claudeModel) Name() string { return m.cfg.Model }

func (m *claudeModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		params, err := m.buildParams(req)
		if err != nil {
			yield(nil, err)
			return
		}
		if !stream {
			msg, err := m.client.Messages.New(ctx, params)
			if err != nil {
				yield(nil, wrapAPIError(err))
				return
			}
			yield(messageToResponse(msg), nil)
			return
		}

		s := m.client.Messages.NewStreaming(ctx, params)
		defer s.Close()
		var msg anthropic.Message
		for s.Next() {
			ev := s.Current()
			if err := msg.Accumulate(ev); err != nil {
				yield(nil, err)
				return
			}
			delta, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent)
			if !ok {
				continue
			}
			if text, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && text.Text != "" {
				partial := &model.LLMResponse{Content: genai.NewContentFromText(text.Text, genai.RoleModel), Partial: true}
				if !yield(partial, nil) {
					return
				}
			}
		}
		if err := s.Err(); err != nil {
			yield(nil, wrapAPIError(err))
			return
		}
		resp := messageToResponse(&msg)
		resp.TurnComplete = true
		yield(resp, nil)
	}
}

func (m *claudeModel) buildParams(req *model.LLMRequest) (anthropic.MessageNewParams, error) {
	msgs, err := contentsToMessages(req.Contents)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(m.cfg.Model),
		MaxTokens: m.cfg.MaxTokens,
		Messages:  msgs,
	}
	if m.cfg.Model == ModelHaiku55 && m.cfg.Effort != "" {
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(m.cfg.Effort)}
	}
	if req.Config != nil {
		if sys := contentText(req.Config.SystemInstruction); sys != "" {
			params.System = []anthropic.TextBlockParam{{Text: sys}}
		}
		tools, err := toolsToParams(req.Config.Tools)
		if err != nil {
			return params, err
		}
		params.Tools = tools
	}
	return params, nil
}

// contentsToMessages converts ADK/genai history to Anthropic messages. It merges
// consecutive turns with the same role, and makes sure the conversation starts
// and ends with a user turn (Claude does not accept assistant prefill).
func contentsToMessages(contents []*genai.Content) ([]anthropic.MessageParam, error) {
	type turn struct {
		assistant bool
		blocks    []anthropic.ContentBlockParamUnion
	}
	var turns []turn
	appendBlocks := func(assistant bool, blocks []anthropic.ContentBlockParamUnion) {
		if len(blocks) == 0 {
			return
		}
		if n := len(turns); n > 0 && turns[n-1].assistant == assistant {
			turns[n-1].blocks = append(turns[n-1].blocks, blocks...)
			return
		}
		turns = append(turns, turn{assistant: assistant, blocks: blocks})
	}

	for _, c := range contents {
		if c == nil {
			continue
		}
		assistant := c.Role == genai.RoleModel
		var blocks, results []anthropic.ContentBlockParamUnion
		for _, p := range c.Parts {
			switch {
			case p == nil:
			case p.Thought:
				// Thinking blocks must be passed back unchanged, and only on assistant turns.
				if assistant && len(p.ThoughtSignature) > 0 {
					sig := string(p.ThoughtSignature)
					if data, ok := strings.CutPrefix(sig, redactedPrefix); ok {
						blocks = append(blocks, anthropic.NewRedactedThinkingBlock(data))
					} else {
						blocks = append(blocks, anthropic.NewThinkingBlock(sig, p.Text))
					}
				}
			case p.FunctionCall != nil:
				args := p.FunctionCall.Args
				if args == nil {
					args = map[string]any{}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(p.FunctionCall.ID, args, p.FunctionCall.Name))
			case p.FunctionResponse != nil:
				body, err := json.Marshal(p.FunctionResponse.Response)
				if err != nil {
					return nil, fmt.Errorf("tool result %s: %w", p.FunctionResponse.Name, err)
				}
				_, isErr := p.FunctionResponse.Response["error"]
				results = append(results, anthropic.NewToolResultBlock(p.FunctionResponse.ID, string(body), isErr))
			case p.Text != "":
				blocks = append(blocks, anthropic.NewTextBlock(p.Text))
			}
		}
		if assistant {
			appendBlocks(true, blocks)
			appendBlocks(false, results)
		} else {
			// Tool results must come first in a user turn.
			appendBlocks(false, append(results, blocks...))
		}
	}

	if len(turns) == 0 || turns[0].assistant {
		first := turn{blocks: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock("Handle the requests as specified in the system instructions.")}}
		turns = append([]turn{first}, turns...)
	}
	if turns[len(turns)-1].assistant {
		turns = append(turns, turn{blocks: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock("Continue processing previous requests as instructed. Exit or provide a summary if no more outputs are needed.")}})
	}

	out := make([]anthropic.MessageParam, len(turns))
	for i, t := range turns {
		if t.assistant {
			out[i] = anthropic.NewAssistantMessage(t.blocks...)
		} else {
			out[i] = anthropic.NewUserMessage(t.blocks...)
		}
	}
	return out, nil
}

func toolsToParams(tools []*genai.Tool) ([]anthropic.ToolUnionParam, error) {
	var out []anthropic.ToolUnionParam
	for _, t := range tools {
		if t == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			schema, err := declarationSchema(fd)
			if err != nil {
				return nil, fmt.Errorf("tool %s: %w", fd.Name, err)
			}
			input := anthropic.ToolInputSchemaParam{Properties: schema["properties"], ExtraFields: map[string]any{}}
			if req, ok := schema["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						input.Required = append(input.Required, s)
					}
				}
			}
			for k, v := range schema {
				switch k {
				case "type", "properties", "required", "$schema":
				default:
					input.ExtraFields[k] = v
				}
			}
			tp := anthropic.ToolParam{Name: fd.Name, InputSchema: input}
			if fd.Description != "" {
				tp.Description = anthropic.String(fd.Description)
			}
			out = append(out, anthropic.ToolUnionParam{OfTool: &tp})
		}
	}
	return out, nil
}

// declarationSchema returns the tool's input JSON Schema as a generic map.
func declarationSchema(fd *genai.FunctionDeclaration) (map[string]any, error) {
	var src any = map[string]any{"type": "object"}
	switch {
	case fd.ParametersJsonSchema != nil:
		src = fd.ParametersJsonSchema
	case fd.Parameters != nil:
		src = genaiSchemaToJSON(fd.Parameters)
	}
	b, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// genaiSchemaToJSON converts the OpenAPI-style genai.Schema to JSON Schema.
func genaiSchemaToJSON(s *genai.Schema) map[string]any {
	if s == nil {
		return nil
	}
	m := map[string]any{}
	if s.Type != "" {
		t := strings.ToLower(string(s.Type))
		if s.Nullable != nil && *s.Nullable {
			m["type"] = []string{t, "null"}
		} else {
			m["type"] = t
		}
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	if len(s.Properties) > 0 {
		props := map[string]any{}
		for k, v := range s.Properties {
			props[k] = genaiSchemaToJSON(v)
		}
		m["properties"] = props
	}
	if s.Items != nil {
		m["items"] = genaiSchemaToJSON(s.Items)
	}
	if len(s.AnyOf) > 0 {
		var anyOf []any
		for _, v := range s.AnyOf {
			anyOf = append(anyOf, genaiSchemaToJSON(v))
		}
		m["anyOf"] = anyOf
	}
	return m
}

func messageToResponse(msg *anthropic.Message) *model.LLMResponse {
	content := &genai.Content{Role: genai.RoleModel}
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			if b.Text != "" {
				content.Parts = append(content.Parts, &genai.Part{Text: b.Text})
			}
		case anthropic.ThinkingBlock:
			content.Parts = append(content.Parts, &genai.Part{Thought: true, Text: b.Thinking, ThoughtSignature: []byte(b.Signature)})
		case anthropic.RedactedThinkingBlock:
			content.Parts = append(content.Parts, &genai.Part{Thought: true, ThoughtSignature: []byte(redactedPrefix + b.Data)})
		case anthropic.ToolUseBlock:
			args := map[string]any{}
			if raw := b.JSON.Input.Raw(); raw != "" {
				_ = json.Unmarshal([]byte(raw), &args)
			} else if len(b.Input) > 0 {
				_ = json.Unmarshal(b.Input, &args)
			}
			content.Parts = append(content.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: b.ID, Name: b.Name, Args: args}})
		}
	}

	resp := &model.LLMResponse{
		Content:      content,
		ModelVersion: string(msg.Model),
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(msg.Usage.InputTokens),
			CandidatesTokenCount: int32(msg.Usage.OutputTokens),
			TotalTokenCount:      int32(msg.Usage.InputTokens + msg.Usage.OutputTokens),
		},
		FinishReason: genai.FinishReasonStop,
	}
	switch msg.StopReason {
	case anthropic.StopReasonMaxTokens:
		resp.FinishReason = genai.FinishReasonMaxTokens
	case anthropic.StopReasonRefusal:
		resp.FinishReason = genai.FinishReasonSafety
		resp.ErrorCode = "refusal"
		resp.ErrorMessage = "The model declined to answer this request."
		if !hasVisibleText(content) {
			content.Parts = append(content.Parts, &genai.Part{Text: "I can't help with that request."})
		}
	}
	return resp
}

func hasVisibleText(c *genai.Content) bool {
	for _, p := range c.Parts {
		if !p.Thought && p.Text != "" {
			return true
		}
	}
	return false
}

func contentText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var parts []string
	for _, p := range c.Parts {
		if p != nil && p.Text != "" && !p.Thought {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func wrapAPIError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401, 403:
			return fmt.Errorf("claude: %s is missing or invalid: %w", APIKeySecret, err)
		case 429:
			return fmt.Errorf("claude: rate limited, try again shortly: %w", err)
		}
	}
	return fmt.Errorf("claude: %w", err)
}
