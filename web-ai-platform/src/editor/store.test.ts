import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { AgentRecord, Draft } from "@/api/types"
import { useEditor } from "./store"
import { saveDraft } from "./use-draft"

function record(id: string, name: string, version = 1): AgentRecord {
  return {
    id,
    name,
    description: "",
    instructions: "",
    // The server stores config as jsonb and may return its keys in another order.
    model: { asset: "core.llm.gateway", config: { model: "m", effort: "low" } },
    skills: [],
    tools: [],
    guardrails: [],
    version,
    createdAt: "",
    updatedAt: "",
    updatedByName: "",
    usedBy: [{ id: "orc_1", name: "Care" }],
  }
}

function draft(version = 1, agents = [record("a", "Router"), record("b", "Weather")]): Draft {
  return {
    orchestrationId: "orc_1",
    version,
    updatedAt: "",
    updatedByName: "",
    spec: {
      rootAgent: "a",
      agents: agents.map((a) => a.id),
      edges: [{ from: "a", to: "b", mode: "transfer" }],
      guardrails: [],
      layout: { a: { x: 0, y: 0 }, b: { x: 0, y: 200 } },
    },
    agents,
  }
}

type Call = { method: string; path: string; ifMatch: string | null; body: Record<string, unknown> }

function mockFetch(respond: (c: Call) => { status: number; body: unknown }) {
  const calls: Call[] = []
  vi.stubGlobal("fetch", async (url: string, init: RequestInit) => {
    const call = {
      method: init.method ?? "GET",
      path: url.replace(/^\/api/, ""),
      ifMatch: new Headers(init.headers).get("If-Match"),
      body: JSON.parse(String(init.body ?? "{}")),
    }
    calls.push(call)
    const r = respond(call)
    return new Response(JSON.stringify(r.body), { status: r.status })
  })
  return calls
}

describe("editor store", () => {
  beforeEach(() => useEditor.getState().reset("orc_1", draft()))
  afterEach(() => vi.unstubAllGlobals())

  it("expands the graph with library agents", () => {
    const s = useEditor.getState()
    expect(s.spec?.agents.map((a) => a.name)).toEqual(["Router", "Weather"])
    expect(s.agentRecords.b.version).toBe(1)
  })

  it("saves only the edited agent, then nothing more", async () => {
    // The server echoes the agent with its keys reordered and the name trimmed.
    const calls = mockFetch(() => ({
      status: 200,
      body: { ...record("b", "Weather"), model: { config: { effort: "low", model: "m" }, asset: "core.llm.gateway" }, version: 2 },
    }))
    useEditor.getState().editAgent("b", (a) => void (a.name = "Weather "))
    await saveDraft("orc_1")
    expect(calls.map((c) => `${c.method} ${c.path} ${c.ifMatch}`)).toEqual(['PUT /v1/agents/b "1"'])
    expect(useEditor.getState().agentRecords.b.version).toBe(2)

    await saveDraft("orc_1")
    expect(calls).toHaveLength(1)
  })

  it("saves the graph without touching agents when only the layout changes", async () => {
    const calls = mockFetch(() => ({ status: 200, body: draft(2) }))
    useEditor.getState().edit((s) => void (s.layout.b = { x: 50, y: 200 }))
    await saveDraft("orc_1")
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(["PUT /v1/orchestrations/orc_1/draft"])
    expect(calls[0].body.spec).toMatchObject({ agents: ["a", "b"] })
    expect(useEditor.getState().version).toBe(2)
  })

  it("reports an agent conflict and can load their version", async () => {
    const theirs = { ...record("b", "Forecaster", 3) }
    mockFetch(() => ({ status: 409, body: { error: { code: "version_conflict", message: "changed by Ana" }, agent: theirs } }))
    useEditor.getState().editAgent("b", (a) => void (a.instructions = "mine"))
    await saveDraft("orc_1")
    expect(useEditor.getState().conflict).toMatchObject({ kind: "agent", message: "changed by Ana" })

    useEditor.getState().loadTheirs()
    const s = useEditor.getState()
    expect(s.conflict).toBeNull()
    expect(s.spec?.agents[1]).toMatchObject({ name: "Forecaster", instructions: "" })
    expect(s.agentRecords.b.version).toBe(3)
  })

  it("picks up agents changed elsewhere only when there are no local edits", () => {
    useEditor.getState().refresh(draft(1, [record("a", "Router"), record("b", "Weather v2", 2)]))
    expect(useEditor.getState().spec?.agents[1].name).toBe("Weather v2")

    useEditor.getState().editAgent("a", (a) => void (a.name = "Local"))
    useEditor.getState().refresh(draft(1, [record("a", "Router", 5), record("b", "Weather v3", 3)]))
    expect(useEditor.getState().spec?.agents.map((a) => a.name)).toEqual(["Local", "Weather v2"])
  })

  it("adds a library agent without marking it as edited", () => {
    useEditor.getState().addAgent(record("c", "Refunds"), { x: 10, y: 20 })
    const s = useEditor.getState()
    expect(s.spec?.agents.map((a) => a.id)).toEqual(["a", "b", "c"])
    expect(s.selection).toEqual({ kind: "agent", id: "c" })
    expect(s.saved.agents.c).toBeDefined()
  })
})
