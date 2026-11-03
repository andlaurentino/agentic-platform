// Package core registers cross-cutting assets available to every domain.
package core

import (
	"agentic-platform/agent-assets/assets/core/guardrails"
	"agentic-platform/agent-assets/assets/core/llms"
	"agentic-platform/agent-assets/assets/core/mcps"
	"agentic-platform/agent-assets/assets/core/tools"
	"agentic-platform/agent-assets/internal/registry"
)

func Register(r *registry.Registry) {
	r.MustLLM(llms.ClaudeHaiku{})
	r.MustTool(tools.CurrentTime{})
	r.MustMCP(mcps.Demo{})
	r.MustGuardrail(guardrails.PIIRedact{})
	r.MustGuardrail(guardrails.PromptInjection{})
}
