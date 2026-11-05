// Package core registers cross-cutting assets available to every domain.
package core

import (
	"agentic-platform/lib-agent-assets/assets/core/guardrails"
	"agentic-platform/lib-agent-assets/assets/core/mcps"
	"agentic-platform/lib-agent-assets/assets/core/tools"
	"agentic-platform/lib-agent-assets/pkg/registry"
)

// Register adds the core tools, MCPs and guardrails. LLM providers are not
// assets: the platform registers the LLM gateways it connects to.
func Register(r *registry.Registry) {
	r.MustTool(tools.CurrentTime{})
	r.MustMCP(mcps.Demo{})
	r.MustGuardrail(guardrails.PIIRedact{})
	r.MustGuardrail(guardrails.PromptInjection{})
}
