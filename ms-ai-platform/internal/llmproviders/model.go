package llmproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/respjson"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// openaiModel adapts the OpenAI Chat Completions protocol to an ADK model.
//
// It also round-trips the reasoning extension ms-llm-gateway uses for Claude
// (the same one LiteLLM uses): responses carry reasoning_content and signed
// thinking_blocks, which become genai thought parts and are sent back on the
// assistant message, so a model that thinks can keep using tools.
type openaiModel struct {
	client          openai.Client
	model           string
	maxTokens       int64
	reasoningEffort string
	streamUsage     bool
	provider        string // for error messages
	baseURL         string
	apiKeyEnv       string
}

func (m *openaiModel) Name() string { return m.model }

func (m *openaiModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		params, err := m.buildParams(req)
		if err != nil {
			yield(nil, err)
			return
		}
		if !stream {
			c, err := m.client.Chat.Completions.New(ctx, params)
			if err != nil {
				yield(nil, m.wrapError(err))
				return
			}
			resp := completionToResponse(c)
			if len(c.Choices) > 0 {
				extra := c.Choices[0].Message.JSON.ExtraFields
				prependThoughts(resp.Content, rawField(extra, "reasoning_content"), rawField(extra, "thinking_blocks"))
			}
			yield(resp, nil)
			return
		}

		if m.streamUsage {
			params.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
		}
		s := m.client.Chat.Completions.NewStreaming(ctx, params)
		defer s.Close()
		acc := openai.ChatCompletionAccumulator{}
		var reasoning strings.Builder
		var blocks []json.RawMessage
		for s.Next() {
			chunk := s.Current()
			acc.AddChunk(chunk)
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta
			if r := rawField(delta.JSON.ExtraFields, "reasoning_content"); r != "" {
				var text string
				if json.Unmarshal([]byte(r), &text) == nil {
					reasoning.WriteString(text)
				}
			}
			if r := rawField(delta.JSON.ExtraFields, "thinking_blocks"); r != "" {
				var bs []json.RawMessage
				if json.Unmarshal([]byte(r), &bs) == nil {
					blocks = append(blocks, bs...)
				}
			}
			if delta.Content != "" {
				partial := &model.LLMResponse{Content: genai.NewContentFromText(delta.Content, genai.RoleModel), Partial: true}
				if !yield(partial, nil) {
					return
				}
			}
		}
		if err := s.Err(); err != nil {
			yield(nil, m.wrapError(err))
			return
		}
		resp := completionToResponse(&acc.ChatCompletion)
		reasoningJSON, _ := json.Marshal(reasoning.String())
		blocksJSON, _ := json.Marshal(blocks)
		prependThoughts(resp.Content, string(reasoningJSON), string(blocksJSON))
		resp.TurnComplete = true
		yield(resp, nil)
	}
}

func rawField(fields map[string]respjson.Field, name string) string {
	if f, ok := fields[name]; ok {
		return f.Raw()
	}
	return ""
}

// prependThoughts adds the reasoning extension as thought parts. Signed thinking
// blocks keep their JSON in ThoughtSignature so they can be sent back as is;
// plain reasoning text (from endpoints without blocks) is kept for display only.
func prependThoughts(content *genai.Content, reasoningJSON, blocksJSON string) {
	var thoughts []*genai.Part
	var blocks []json.RawMessage
	if blocksJSON != "" && json.Unmarshal([]byte(blocksJSON), &blocks) == nil {
		for _, b := range blocks {
			var block struct {
				Thinking string `json:"thinking"`
			}
			_ = json.Unmarshal(b, &block)
			thoughts = append(thoughts, &genai.Part{Thought: true, Text: block.Thinking, ThoughtSignature: []byte(b)})
		}
	}
	if len(thoughts) == 0 && reasoningJSON != "" {
		var text string
		if json.Unmarshal([]byte(reasoningJSON), &text) == nil && text != "" {
			thoughts = append(thoughts, &genai.Part{Thought: true, Text: text})
		}
	}
	content.Parts = append(thoughts, content.Parts...)
}

func (m *openaiModel) buildParams(req *model.LLMRequest) (openai.ChatCompletionNewParams, error) {
	var system string
	var tools []*genai.Tool
	if req.Config != nil {
		system = contentText(req.Config.SystemInstruction)
		tools = req.Config.Tools
	}
	msgs, err := contentsToOpenAI(system, req.Contents)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	params := openai.ChatCompletionNewParams{
		Model:               openai.ChatModel(m.model),
		Messages:            msgs,
		MaxCompletionTokens: openai.Int(m.maxTokens),
	}
	if m.reasoningEffort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(m.reasoningEffort)
	}
	if params.Tools, err = toolsToOpenAI(tools); err != nil {
		return params, err
	}
	return params, nil
}

// contentsToOpenAI converts ADK/genai history to Chat Completions messages.
// Signed thinking blocks go back on their assistant message as thinking_blocks;
// other thought parts are dropped.
func contentsToOpenAI(system string, contents []*genai.Content) ([]openai.ChatCompletionMessageParamUnion, error) {
	var out []openai.ChatCompletionMessageParamUnion
	if system != "" {
		out = append(out, openai.SystemMessage(system))
	}
	for _, c := range contents {
		if c == nil {
			continue
		}
		var text strings.Builder
		var calls []openai.ChatCompletionMessageToolCallUnionParam
		var results []openai.ChatCompletionMessageParamUnion
		var blocks []json.RawMessage
		for _, p := range c.Parts {
			switch {
			case p == nil:
			case p.Thought:
				if json.Valid(p.ThoughtSignature) {
					blocks = append(blocks, json.RawMessage(p.ThoughtSignature))
				}
			case p.FunctionCall != nil:
				args, err := json.Marshal(p.FunctionCall.Args)
				if err != nil {
					return nil, fmt.Errorf("tool call %s: %w", p.FunctionCall.Name, err)
				}
				if p.FunctionCall.Args == nil {
					args = []byte("{}")
				}
				calls = append(calls, openai.ChatCompletionMessageToolCallUnionParam{OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID:       p.FunctionCall.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{Name: p.FunctionCall.Name, Arguments: string(args)},
				}})
			case p.FunctionResponse != nil:
				body, err := json.Marshal(p.FunctionResponse.Response)
				if err != nil {
					return nil, fmt.Errorf("tool result %s: %w", p.FunctionResponse.Name, err)
				}
				results = append(results, openai.ToolMessage(string(body), p.FunctionResponse.ID))
			case p.Text != "":
				if text.Len() > 0 {
					text.WriteString("\n\n")
				}
				text.WriteString(p.Text)
			}
		}
		if c.Role == genai.RoleModel {
			if text.Len() > 0 || len(calls) > 0 {
				a := openai.ChatCompletionAssistantMessageParam{ToolCalls: calls}
				if text.Len() > 0 {
					a.Content.OfString = openai.String(text.String())
				}
				if len(blocks) > 0 {
					a.SetExtraFields(map[string]any{"thinking_blocks": blocks})
				}
				out = append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &a})
			}
			out = append(out, results...)
			continue
		}
		// Tool results must directly follow the assistant message that made the calls.
		out = append(out, results...)
		if text.Len() > 0 {
			out = append(out, openai.UserMessage(text.String()))
		}
	}
	return out, nil
}

func toolsToOpenAI(tools []*genai.Tool) ([]openai.ChatCompletionToolUnionParam, error) {
	var out []openai.ChatCompletionToolUnionParam
	for _, t := range tools {
		if t == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			schema, err := declarationSchema(fd)
			if err != nil {
				return nil, fmt.Errorf("tool %s: %w", fd.Name, err)
			}
			delete(schema, "$schema")
			schema["type"] = "object"
			if _, ok := schema["properties"]; !ok {
				schema["properties"] = map[string]any{}
			}
			def := shared.FunctionDefinitionParam{Name: fd.Name, Parameters: shared.FunctionParameters(schema)}
			if fd.Description != "" {
				def.Description = openai.String(fd.Description)
			}
			out = append(out, openai.ChatCompletionFunctionTool(def))
		}
	}
	return out, nil
}

func completionToResponse(c *openai.ChatCompletion) *model.LLMResponse {
	content := &genai.Content{Role: genai.RoleModel}
	resp := &model.LLMResponse{Content: content, ModelVersion: c.Model, FinishReason: genai.FinishReasonStop}
	if len(c.Choices) > 0 {
		choice := c.Choices[0]
		msg := choice.Message
		if msg.Content != "" {
			content.Parts = append(content.Parts, &genai.Part{Text: msg.Content})
		}
		if msg.Refusal != "" {
			content.Parts = append(content.Parts, &genai.Part{Text: msg.Refusal})
		}
		for _, tc := range msg.ToolCalls {
			args := map[string]any{}
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					// Usually a truncated call (finish_reason length); the tool then reports missing arguments.
					slog.Warn("model returned invalid tool arguments", "tool", tc.Function.Name, "err", err)
				}
			}
			content.Parts = append(content.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: tc.ID, Name: tc.Function.Name, Args: args}})
		}
		switch choice.FinishReason {
		case "length":
			resp.FinishReason = genai.FinishReasonMaxTokens
		case "content_filter":
			resp.FinishReason = genai.FinishReasonSafety
			resp.ErrorCode = "content_filter"
			resp.ErrorMessage = "The response was blocked by the provider's content filter."
			if !hasVisibleText(content) {
				content.Parts = append(content.Parts, &genai.Part{Text: "I can't help with that request."})
			}
		}
	}
	if u := c.Usage; u.TotalTokens > 0 || u.PromptTokens > 0 {
		resp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(u.PromptTokens),
			CandidatesTokenCount: int32(u.CompletionTokens),
			TotalTokenCount:      int32(u.TotalTokens),
		}
	}
	return resp
}

func (m *openaiModel) wrapError(err error) error {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("%s is unreachable at %s: %w", m.provider, m.baseURL, err)
	}
	msg := apiErr.Message
	if msg == "" {
		msg = err.Error()
	}
	switch apiErr.StatusCode {
	case 401, 403:
		if m.apiKeyEnv != "" {
			return fmt.Errorf("%s: %s is missing or invalid: %s", m.provider, m.apiKeyEnv, msg)
		}
		return fmt.Errorf("%s: request was not authorized: %s", m.provider, msg)
	case 404:
		return fmt.Errorf("%s: model %q is not available: %s", m.provider, m.model, msg)
	case 429:
		return fmt.Errorf("%s: rate limited, try again shortly: %s", m.provider, msg)
	}
	return fmt.Errorf("%s: %s", m.provider, msg)
}
