import type { AgentRecord, AgentSpec, Draft, DraftGraph, OrchestrationSpec } from "@/api/types"

/** Fields for a new agent; the server assigns the id. */
export function newAgent(spec: OrchestrationSpec | null): Partial<AgentSpec> {
  const n = (spec?.agents.length ?? 0) + 1
  return {
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

/** The editable part of a library agent. */
export function agentSpec(r: AgentRecord): AgentSpec {
  const { id, name, description, instructions, model, skills, tools, guardrails } = r
  return structuredClone({ id, name, description, instructions, model, skills, tools, guardrails })
}

/** Resolves a draft's graph with its library agents into the shape the editor works on. */
export function expandDraft(draft: Draft): OrchestrationSpec {
  const byId = new Map(draft.agents.map((a) => [a.id, agentSpec(a)]))
  return {
    ...structuredClone(draft.spec),
    agents: draft.spec.agents.flatMap((id) => byId.get(id) ?? []),
  }
}

export function toGraph(spec: OrchestrationSpec): DraftGraph {
  return { ...spec, agents: spec.agents.map((a) => a.id) }
}

/** JSON with sorted keys, so equal values compare equal whatever order the server returns keys in. */
export function canonical(v: unknown): string {
  return JSON.stringify(v, (_, x) =>
    x && typeof x === "object" && !Array.isArray(x)
      ? Object.fromEntries(Object.keys(x as object).sort().map((k) => [k, (x as Record<string, unknown>)[k]]))
      : x,
  )
}

/** Takes an agent off the canvas (it stays in the library), with its connections and position. */
export function removeAgent(spec: OrchestrationSpec, id: string) {
  if (id === spec.rootAgent) return
  spec.agents = spec.agents.filter((a) => a.id !== id)
  spec.edges = spec.edges.filter((e) => e.from !== id && e.to !== id)
  delete spec.layout[id]
}

export function removeEdge(spec: OrchestrationSpec, from: string, to: string) {
  spec.edges = spec.edges.filter((e) => !(e.from === from && e.to === to))
}

export function makeRoot(spec: OrchestrationSpec, id: string) {
  spec.rootAgent = id
  // The root cannot have a parent.
  spec.edges = spec.edges.filter((e) => e.to !== id)
}
