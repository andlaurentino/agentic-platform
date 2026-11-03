import { newId } from "@/api/client"
import type { AgentSpec, OrchestrationSpec } from "@/api/types"

export function newAgent(spec: OrchestrationSpec): AgentSpec {
  const n = spec.agents.length + 1
  return {
    id: newId("agt"),
    name: `Agent ${n}`,
    description: "",
    instructions: "",
    model: { asset: "core.llm.claude-haiku", config: { model: "claude-haiku-5-5" } },
    skills: [],
    tools: [],
    guardrails: [],
  }
}
