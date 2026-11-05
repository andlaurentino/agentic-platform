package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/v2/option"

	"agentic-platform/ms-llm-gateway/internal/llmconfig"
	"agentic-platform/ms-llm-gateway/internal/wire"
)

// anthropicChat serves an OpenAI Chat Completions request from the Anthropic
// Messages API.
func (p *Provider) anthropicChat(ctx context.Context, c Call, out Output) (Result, *wire.Error) {
	params, werr := anthropicParams(c, p.Cfg)
	if werr != nil {
		return Result{}, werr
	}
	opts := []anthropicopt.RequestOption{anthropicopt.WithAPIKey(p.Cfg.APIKey())}
	for k, v := range p.Cfg.ExpandedHeaders() {
		opts = append(opts, anthropicopt.WithHeader(k, v))
	}
	created := time.Now().Unix()

	if !c.Req.Stream {
		msg, err := p.anthropic.Messages.New(ctx, params, opts...)
		if err != nil {
			return Result{}, p.anthropicError(err, c.Model.ID)
		}
		resp := completionFromMessage(msg, c.ClientModel, created)
		wire.WriteJSON(out.W, resp)
		return Result{Usage: *resp.Usage}, nil
	}

	s := p.anthropic.Messages.NewStreaming(ctx, params, opts...)
	defer s.Close()
	tr := &anthropicStream{sse: out.SSE, model: c.ClientModel, created: created, tools: map[int64]int{}, thinking: map[int64]*wire.ThinkingBlock{}}
	var msg anthropic.Message
	for s.Next() {
		ev := s.Current()
		if err := msg.Accumulate(ev); err != nil {
			return Result{}, wire.Errorf(http.StatusBadGateway, "upstream_error", "", "%s: %v", p.Cfg.Name, err)
		}
		if err := tr.event(ev); err != nil {
			return Result{}, nil // client went away
		}
	}
	if err := s.Err(); err != nil {
		return Result{}, p.anthropicError(err, c.Model.ID)
	}
	usage := usageFromAnthropic(msg.Usage)
	if err := tr.finish(c.Req.IncludeUsage(), usage); err != nil {
		return Result{Usage: usage}, nil
	}
	return Result{Usage: usage}, nil
}

// anthropicParams converts a Chat Completions request to Messages API params.
func anthropicParams(c Call, cfg llmconfig.Provider) (anthropic.MessageNewParams, *wire.Error) {
	r := c.Req
	if r.N != nil && *r.N > 1 {
		return anthropic.MessageNewParams{}, wire.BadRequest("n > 1 is not supported for %s models", cfg.Name)
	}
	if len(r.ResponseFormat) > 0 && string(r.ResponseFormat) != "null" {
		var rf struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(r.ResponseFormat, &rf)
		if rf.Type != "" && rf.Type != "text" {
			return anthropic.MessageNewParams{}, wire.BadRequest("response_format %q is not supported for %s models", rf.Type, cfg.Name)
		}
	}

	system, msgs, werr := toAnthropicMessages(r.Messages)
	if werr != nil {
		return anthropic.MessageNewParams{}, werr
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.Model.ID),
		MaxTokens: cfg.Params.MaxTokens,
		Messages:  msgs,
	}
	if limit := r.OutputLimit(); limit > 0 {
		params.MaxTokens = limit
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}
	if c.Model.Effort {
		effort := cfg.Params.Effort
		if r.ReasoningEffort != "" {
			level, ok := map[string]string{"minimal": "low", "low": "low", "medium": "medium", "high": "high"}[r.ReasoningEffort]
			if !ok {
				return params, wire.BadRequest("reasoning_effort must be low, medium or high, got %q", r.ReasoningEffort)
			}
			effort = level
		}
		if effort != "" {
			params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(effort)}
		}
	}
	// temperature and top_p are ignored: current Claude models don't take sampling parameters.
	stops, err := r.StopSequences()
	if err != nil {
		return params, wire.BadRequest("%v", err)
	}
	params.StopSequences = stops

	tools, werr := toAnthropicTools(r.Tools)
	if werr != nil {
		return params, werr
	}
	params.Tools = tools
	if len(tools) > 0 {
		choice, werr := toAnthropicToolChoice(r.ToolChoice, r.ParallelToolCalls)
		if werr != nil {
			return params, werr
		}
		params.ToolChoice = choice
	}
	return params, nil
}

// toAnthropicMessages converts chat messages. System and developer messages
// become the system prompt; consecutive turns with the same role are merged;
// tool results come first in their user turn; and the conversation starts and
// ends with a user turn, since Claude does not accept assistant prefill.
func toAnthropicMessages(in []wire.Message) (string, []anthropic.MessageParam, *wire.Error) {
	type turn struct {
		assistant bool
		results   []anthropic.ContentBlockParamUnion
		blocks    []anthropic.ContentBlockParamUnion
	}
	var turns []turn
	add := func(assistant bool, results, blocks []anthropic.ContentBlockParamUnion) {
		if len(results)+len(blocks) == 0 {
			return
		}
		if n := len(turns); n > 0 && turns[n-1].assistant == assistant {
			turns[n-1].results = append(turns[n-1].results, results...)
			turns[n-1].blocks = append(turns[n-1].blocks, blocks...)
			return
		}
		turns = append(turns, turn{assistant: assistant, results: results, blocks: blocks})
	}

	var system []string
	for _, m := range in {
		text, err := m.Text()
		if err != nil {
			return "", nil, wire.BadRequest("%v", err)
		}
		switch m.Role {
		case "system", "developer":
			if text != "" {
				system = append(system, text)
			}
		case "user":
			if text != "" {
				add(false, nil, []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(text)})
			}
		case "tool":
			if m.ToolCallID == "" {
				return "", nil, wire.BadRequest("tool message needs tool_call_id")
			}
			add(false, []anthropic.ContentBlockParamUnion{anthropic.NewToolResultBlock(m.ToolCallID, text, false)}, nil)
		case "assistant":
			var blocks []anthropic.ContentBlockParamUnion
			// Thinking blocks must be passed back unchanged, before the rest of the turn.
			for _, tb := range m.ThinkingBlocks {
				switch tb.Type {
				case "thinking":
					blocks = append(blocks, anthropic.NewThinkingBlock(tb.Signature, tb.Thinking))
				case "redacted_thinking":
					blocks = append(blocks, anthropic.NewRedactedThinkingBlock(tb.Data))
				}
			}
			if text != "" {
				blocks = append(blocks, anthropic.NewTextBlock(text))
			}
			for _, tc := range m.ToolCalls {
				var input any = map[string]any{}
				if args := strings.TrimSpace(tc.Function.Arguments); args != "" && json.Valid([]byte(args)) {
					input = json.RawMessage(args)
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Function.Name))
			}
			add(true, nil, blocks)
		default:
			return "", nil, wire.BadRequest("unsupported message role %q", m.Role)
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
		blocks := append(t.results, t.blocks...)
		if t.assistant {
			out[i] = anthropic.NewAssistantMessage(blocks...)
		} else {
			out[i] = anthropic.NewUserMessage(blocks...)
		}
	}
	return strings.Join(system, "\n\n"), out, nil
}

func toAnthropicTools(tools []wire.Tool) ([]anthropic.ToolUnionParam, *wire.Error) {
	var out []anthropic.ToolUnionParam
	for _, t := range tools {
		if t.Type != "function" {
			return nil, wire.BadRequest("tool type %q is not supported", t.Type)
		}
		schema := map[string]any{}
		if len(t.Function.Parameters) > 0 && string(t.Function.Parameters) != "null" {
			if err := json.Unmarshal(t.Function.Parameters, &schema); err != nil {
				return nil, wire.BadRequest("tool %s: parameters must be a JSON Schema object", t.Function.Name)
			}
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
		tp := anthropic.ToolParam{Name: t.Function.Name, InputSchema: input}
		if t.Function.Description != "" {
			tp.Description = anthropic.String(t.Function.Description)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tp})
	}
	return out, nil
}

// toAnthropicToolChoice maps tool_choice: auto, none, required (any), or a named function.
func toAnthropicToolChoice(raw json.RawMessage, parallel *bool) (anthropic.ToolChoiceUnionParam, *wire.Error) {
	var choice anthropic.ToolChoiceUnionParam
	mode := "auto"
	var name string
	if len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &mode) != nil {
			var obj struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if err := json.Unmarshal(raw, &obj); err != nil || obj.Function.Name == "" {
				return choice, wire.BadRequest("tool_choice must be auto, none, required or {type: function, function: {name}}")
			}
			mode, name = "function", obj.Function.Name
		}
	}
	single := parallel != nil && !*parallel
	switch mode {
	case "auto":
		c := anthropic.ToolChoiceAutoParam{}
		if single {
			c.DisableParallelToolUse = anthropic.Bool(true)
		}
		choice.OfAuto = &c
	case "none":
		choice.OfNone = &anthropic.ToolChoiceNoneParam{}
	case "required":
		c := anthropic.ToolChoiceAnyParam{}
		if single {
			c.DisableParallelToolUse = anthropic.Bool(true)
		}
		choice.OfAny = &c
	case "function":
		c := anthropic.ToolChoiceToolParam{Name: name}
		if single {
			c.DisableParallelToolUse = anthropic.Bool(true)
		}
		choice.OfTool = &c
	default:
		return choice, wire.BadRequest("tool_choice %q is not supported", mode)
	}
	return choice, nil
}

func finishReason(stop anthropic.StopReason) string {
	switch stop {
	case anthropic.StopReasonToolUse:
		return "tool_calls"
	case anthropic.StopReasonMaxTokens, "model_context_window_exceeded":
		return "length"
	case anthropic.StopReasonRefusal:
		return "content_filter"
	}
	return "stop"
}

func usageFromAnthropic(u anthropic.Usage) wire.Usage {
	in := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	return wire.Usage{PromptTokens: in, CompletionTokens: u.OutputTokens, TotalTokens: in + u.OutputTokens}
}

// completionFromMessage converts a full Messages API response.
func completionFromMessage(msg *anthropic.Message, model string, created int64) wire.ChatCompletion {
	out := wire.ResponseMessage{Role: "assistant"}
	var text, reasoning strings.Builder
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			text.WriteString(b.Text)
		case anthropic.ThinkingBlock:
			reasoning.WriteString(b.Thinking)
			out.ThinkingBlocks = append(out.ThinkingBlocks, wire.ThinkingBlock{Type: "thinking", Thinking: b.Thinking, Signature: b.Signature})
		case anthropic.RedactedThinkingBlock:
			out.ThinkingBlocks = append(out.ThinkingBlocks, wire.ThinkingBlock{Type: "redacted_thinking", Data: b.Data})
		case anthropic.ToolUseBlock:
			args := b.JSON.Input.Raw()
			if args == "" {
				args = string(b.Input)
			}
			if args == "" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, wire.ToolCall{ID: b.ID, Type: "function", Function: wire.FunctionCall{Name: b.Name, Arguments: args}})
		}
	}
	if text.Len() > 0 || len(out.ToolCalls) == 0 {
		out.Content = wire.Ptr(text.String())
	}
	out.ReasoningContent = reasoning.String()
	usage := usageFromAnthropic(msg.Usage)
	return wire.ChatCompletion{
		ID: "chatcmpl-" + msg.ID, Object: "chat.completion", Created: created, Model: model,
		Choices: []wire.Choice{{Message: out, FinishReason: finishReason(msg.StopReason)}},
		Usage:   &usage,
	}
}

// anthropicStream translates Messages API stream events to chat.completion.chunk events.
type anthropicStream struct {
	sse      *wire.SSE
	id       string
	model    string
	created  int64
	tools    map[int64]int // content block index → tool call index
	thinking map[int64]*wire.ThinkingBlock
	stop     anthropic.StopReason
}

func (t *anthropicStream) send(d wire.Delta, finish *string) error {
	return t.sse.Send(wire.Chunk{
		ID: t.id, Object: "chat.completion.chunk", Created: t.created, Model: t.model,
		Choices: []wire.ChunkChoice{{Delta: d, FinishReason: finish}},
	})
}

func (t *anthropicStream) event(ev anthropic.MessageStreamEventUnion) error {
	switch e := ev.AsAny().(type) {
	case anthropic.MessageStartEvent:
		t.id = "chatcmpl-" + e.Message.ID
		return t.send(wire.Delta{Role: "assistant", Content: wire.Ptr("")}, nil)
	case anthropic.ContentBlockStartEvent:
		switch b := e.ContentBlock.AsAny().(type) {
		case anthropic.ToolUseBlock:
			i := len(t.tools)
			t.tools[e.Index] = i
			return t.send(wire.Delta{ToolCalls: []wire.ToolCall{{Index: &i, ID: b.ID, Type: "function", Function: wire.FunctionCall{Name: b.Name}}}}, nil)
		case anthropic.ThinkingBlock:
			t.thinking[e.Index] = &wire.ThinkingBlock{Type: "thinking", Thinking: b.Thinking, Signature: b.Signature}
		case anthropic.RedactedThinkingBlock:
			return t.send(wire.Delta{ThinkingBlocks: []wire.ThinkingBlock{{Type: "redacted_thinking", Data: b.Data}}}, nil)
		case anthropic.TextBlock:
			if b.Text != "" {
				return t.send(wire.Delta{Content: wire.Ptr(b.Text)}, nil)
			}
		}
	case anthropic.ContentBlockDeltaEvent:
		switch d := e.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			if d.Text != "" {
				return t.send(wire.Delta{Content: wire.Ptr(d.Text)}, nil)
			}
		case anthropic.InputJSONDelta:
			if i, ok := t.tools[e.Index]; ok && d.PartialJSON != "" {
				return t.send(wire.Delta{ToolCalls: []wire.ToolCall{{Index: &i, Function: wire.FunctionCall{Arguments: d.PartialJSON}}}}, nil)
			}
		case anthropic.ThinkingDelta:
			if tb := t.thinking[e.Index]; tb != nil {
				tb.Thinking += d.Thinking
			}
			if d.Thinking != "" {
				return t.send(wire.Delta{ReasoningContent: d.Thinking}, nil)
			}
		case anthropic.SignatureDelta:
			if tb := t.thinking[e.Index]; tb != nil {
				tb.Signature += d.Signature
			}
		}
	case anthropic.ContentBlockStopEvent:
		// The complete, signed block is what the client must send back.
		if tb := t.thinking[e.Index]; tb != nil {
			delete(t.thinking, e.Index)
			return t.send(wire.Delta{ThinkingBlocks: []wire.ThinkingBlock{*tb}}, nil)
		}
	case anthropic.MessageDeltaEvent:
		t.stop = e.Delta.StopReason
	}
	return nil
}

func (t *anthropicStream) finish(includeUsage bool, usage wire.Usage) error {
	if err := t.send(wire.Delta{}, wire.Ptr(finishReason(t.stop))); err != nil {
		return err
	}
	if includeUsage {
		if err := t.sse.Send(wire.Chunk{ID: t.id, Object: "chat.completion.chunk", Created: t.created, Model: t.model, Choices: []wire.ChunkChoice{}, Usage: &usage}); err != nil {
			return err
		}
	}
	return t.sse.Done()
}

func (p *Provider) anthropicError(err error, modelID string) *wire.Error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return p.upstreamError(apiErr.StatusCode, upstreamMessage([]byte(apiErr.RawJSON())), modelID)
	}
	return p.unreachable(err)
}
