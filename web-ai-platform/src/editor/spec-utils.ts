import { newId } from "@/api/client"
import type { AgentSpec, OrchestrationSpec } from "@/api/types"

export function newAgent(spec: OrchestrationSpec): AgentSpec {
  const n = spec.agents.length + 1
  return {
    id: newId("agt"),
    name: `Agent ${n}`,
    description: "",
    instructions: "",
    // The platform's LLM gateway; an empty config uses its default model.
    model: { asset: "core.llm.gateway", config: {} },
    skills: [],
    tools: [],
    guardrails: [],
  }
}
