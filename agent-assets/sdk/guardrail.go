package sdk

import (
	"context"
	"encoding/json"

	"google.golang.org/adk/model"
)

// GuardrailLevel separates fast deterministic checks from model-based ones.
type GuardrailLevel string

const (
	L1 GuardrailLevel = "L1" // deterministic: regex, lists, limits
	L2 GuardrailLevel = "L2" // model-based: classifiers
)

// Stage is where in a run a guardrail is applied.
type Stage string

const (
	StageInput      Stage = "input"
	StageOutput     Stage = "output"
	StageToolCall   Stage = "tool_call"
	StageToolResult Stage = "tool_result"
)

// Action is the outcome of a check.
type Action string

const (
	ActionAllow    Action = "allow"
	ActionRedact   Action = "redact"
	ActionBlock    Action = "block"
	ActionEscalate Action = "escalate"
)

// GuardInput is the text being checked.
type GuardInput struct {
	Stage Stage
	Text  string
}

// GuardResult is what a check decided.
type GuardResult struct {
	Action   Action   `json:"action"`
	Redacted string   `json:"redacted,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Labels   []string `json:"labels,omitempty"`
}

// Check runs one guardrail against one input.
type Check func(ctx context.Context, in GuardInput) (GuardResult, error)

// Deps are platform services a guardrail may use.
type Deps struct {
	Secrets Secrets
	// LLM returns a model for L2 guardrails. modelID is a model name the LLM asset accepts.
	LLM func(ctx context.Context, modelID string) (model.LLM, error)
}

// Guardrail is a policy check that runs around model and tool calls.
type Guardrail interface {
	Asset
	Level() GuardrailLevel
	Stages() []Stage
	New(ctx context.Context, cfg json.RawMessage, deps Deps) (Check, error)
}
