// Package guardrails runs guardrail assets as ADK model callbacks.
package guardrails

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"agentic-platform/agent-assets/sdk"
)

// BlockedMessage is what the user sees when a guardrail blocks a message.
const BlockedMessage = "Sorry, I can't help with that request."

// RemovedMessage replaces a blocked user message in later model requests, so a
// blocked prompt injection never reaches a model in a later turn.
const RemovedMessage = "[message removed: blocked by a guardrail]"

// blockedStateKey is the session-state key holding hashes of blocked user messages.
const blockedStateKey = "guardrails:blocked"

func textHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func blockedHashes(cctx agent.CallbackContext) []string {
	v, err := cctx.State().Get(blockedStateKey)
	if err != nil {
		return nil
	}
	switch hs := v.(type) {
	case []string:
		return hs
	case []any:
		out := make([]string, 0, len(hs))
		for _, h := range hs {
			if s, ok := h.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Decision is reported to the UI for every guardrail that did something.
type Decision struct {
	GuardrailID string             `json:"guardrailId"`
	Name        string             `json:"name"`
	Level       sdk.GuardrailLevel `json:"level"`
	Stage       sdk.Stage          `json:"stage"`
	Action      sdk.Action         `json:"action"`
	Reason      string             `json:"reason,omitempty"`
	Labels      []string           `json:"labels,omitempty"`
	AgentID     string             `json:"agentId,omitempty"`
}

// Bound is a guardrail asset with its config applied.
type Bound struct {
	ID     string
	Name   string
	Level  sdk.GuardrailLevel
	Stages []sdk.Stage
	Check  sdk.Check
}

func (b Bound) runsAt(s sdk.Stage) bool { return slices.Contains(b.Stages, s) }

// Run holds state shared by every agent's callbacks during one run: a result
// cache (ADK rebuilds the request before each model call, so the same text is
// seen many times) and the sink that reports decisions.
type Run struct {
	sink     func(Decision)
	mu       sync.Mutex
	results  map[string]sdk.GuardResult
	reported map[string]bool
}

func NewRun(sink func(Decision)) *Run {
	if sink == nil {
		sink = func(Decision) {}
	}
	return &Run{sink: sink, results: map[string]sdk.GuardResult{}, reported: map[string]bool{}}
}

func (r *Run) check(ctx context.Context, b Bound, stage sdk.Stage, text string) sdk.GuardResult {
	key := b.ID + "\x00" + string(stage) + "\x00" + text
	r.mu.Lock()
	res, ok := r.results[key]
	r.mu.Unlock()
	if ok {
		return res
	}
	res, err := b.Check(ctx, sdk.GuardInput{Stage: stage, Text: text})
	if err != nil {
		res = sdk.GuardResult{Action: sdk.ActionBlock, Reason: fmt.Sprintf("guardrail failed: %v", err), Labels: []string{"guardrail_error"}}
	}
	r.mu.Lock()
	r.results[key] = res
	r.mu.Unlock()
	return res
}

func (r *Run) report(b Bound, agentID string, stage sdk.Stage, text string, res sdk.GuardResult) {
	if res.Action == sdk.ActionAllow || res.Action == "" {
		return
	}
	key := b.ID + "\x00" + string(stage) + "\x00" + text
	r.mu.Lock()
	seen := r.reported[key]
	r.reported[key] = true
	r.mu.Unlock()
	if seen {
		return
	}
	r.sink(Decision{GuardrailID: b.ID, Name: b.Name, Level: b.Level, Stage: stage, Action: res.Action, Reason: res.Reason, Labels: res.Labels, AgentID: agentID})
}

// apply runs L1 checks in order (stopping on block), then L2 checks in parallel.
// It returns the possibly redacted text and whether the text is blocked.
func (r *Run) apply(ctx context.Context, bounds []Bound, stage sdk.Stage, text string, agentID string, withL2, report bool) (string, bool) {
	for _, b := range bounds {
		if b.Level != sdk.L1 || !b.runsAt(stage) {
			continue
		}
		res := r.check(ctx, b, stage, text)
		if report {
			r.report(b, agentID, stage, text, res)
		}
		switch res.Action {
		case sdk.ActionBlock, sdk.ActionEscalate:
			return text, true
		case sdk.ActionRedact:
			text = res.Redacted
		}
	}
	if !withL2 {
		return text, false
	}
	var l2 []Bound
	for _, b := range bounds {
		if b.Level == sdk.L2 && b.runsAt(stage) {
			l2 = append(l2, b)
		}
	}
	results := make([]sdk.GuardResult, len(l2))
	var wg sync.WaitGroup
	for i, b := range l2 {
		wg.Go(func() { results[i] = r.check(ctx, b, stage, text) })
	}
	wg.Wait()
	blocked := false
	for i, b := range l2 {
		if report {
			r.report(b, agentID, stage, text, results[i])
		}
		switch results[i].Action {
		case sdk.ActionBlock, sdk.ActionEscalate:
			blocked = true
		case sdk.ActionRedact:
			text = results[i].Redacted
		}
	}
	return text, blocked
}

func contentText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var s string
	for _, p := range c.Parts {
		if p != nil && !p.Thought {
			s += p.Text
		}
	}
	return s
}

// BeforeModel checks user input. L1 redaction is applied to every user text in
// the request (cheap, keeps history consistent); L2 checks and reporting only
// run on the message that started this run.
func BeforeModel(run *Run, agentID string, bounds []Bound) llmagent.BeforeModelCallback {
	return func(cctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
		latest := contentText(cctx.UserContent())
		blocked := blockedHashes(cctx)
		for ci, c := range req.Contents {
			if c == nil || c.Role != genai.RoleUser {
				continue
			}
			var parts []*genai.Part
			for pi, p := range c.Parts {
				if p == nil || p.Thought || p.Text == "" {
					continue
				}
				if slices.Contains(blocked, textHash(p.Text)) {
					if parts == nil {
						parts = slices.Clone(c.Parts)
					}
					parts[pi] = &genai.Part{Text: RemovedMessage}
					continue
				}
				isLatest := latest != "" && p.Text == latest
				text, isBlocked := run.apply(cctx, bounds, sdk.StageInput, p.Text, agentID, isLatest, isLatest)
				if isBlocked && isLatest {
					if err := cctx.State().Set(blockedStateKey, append(slices.Clone(blocked), textHash(p.Text))); err != nil {
						return nil, err
					}
					return &model.LLMResponse{Content: genai.NewContentFromText(BlockedMessage, genai.RoleModel), TurnComplete: true}, nil
				}
				if text != p.Text {
					if parts == nil {
						parts = slices.Clone(c.Parts)
					}
					parts[pi] = &genai.Part{Text: text}
				}
			}
			if parts != nil {
				// Copy instead of mutating: the request is built from session events.
				req.Contents[ci] = &genai.Content{Role: c.Role, Parts: parts}
			}
		}
		return nil, nil
	}
}

// AfterModel checks model output. Partial (streamed) chunks get L1 redaction
// only; the final response gets every output check and is reported.
func AfterModel(run *Run, agentID string, bounds []Bound) llmagent.AfterModelCallback {
	return func(cctx agent.CallbackContext, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
		if respErr != nil || resp == nil || resp.Content == nil {
			return nil, nil
		}
		final := !resp.Partial
		var parts []*genai.Part
		for pi, p := range resp.Content.Parts {
			if p == nil || p.Thought || p.Text == "" {
				continue
			}
			text, blocked := run.apply(cctx, bounds, sdk.StageOutput, p.Text, agentID, final, final)
			if blocked {
				text = BlockedMessage
			}
			if text != p.Text {
				if parts == nil {
					parts = slices.Clone(resp.Content.Parts)
				}
				parts[pi] = &genai.Part{Text: text}
			}
		}
		if parts == nil {
			return nil, nil
		}
		out := *resp
		out.Content = &genai.Content{Role: resp.Content.Role, Parts: parts}
		return &out, nil
	}
}
