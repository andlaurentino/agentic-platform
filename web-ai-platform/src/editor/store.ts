import { create } from "zustand"
import type { AgentRecord, AgentSpec, Draft, OrchestrationSpec, Problem } from "@/api/types"
import { agentSpec, canonical, expandDraft, toGraph } from "./spec-utils"

export type Selection = { kind: "agent"; id: string } | { kind: "edge"; from: string; to: string } | { kind: "orchestration" }

export type Conflict = { message: string } & ({ kind: "draft"; draft: Draft } | { kind: "agent"; agent: AgentRecord })

/** Canonical JSON of what the server holds, to find what an autosave must send. */
interface Saved {
  graph: string
  agents: Record<string, string>
}

interface EditorState {
  orchestrationId: string | null
  /** The orchestration with its agents resolved from the library. */
  spec: OrchestrationSpec | null
  /** Draft (graph) version. */
  version: number
  /** Library records of the agents on the canvas: their version and where else they are used. */
  agentRecords: Record<string, AgentRecord>
  saved: Saved
  /** Incremented on every local edit; compared with savedSeq to know what is unsaved. */
  editSeq: number
  savedSeq: number
  saving: boolean
  saveError: string | null
  conflict: Conflict | null
  selection: Selection
  /** Agent currently speaking in the playground, highlighted on the canvas. */
  activeAgentId: string | null
  problems: Problem[]

  reset: (orchestrationId: string, draft: Draft) => void
  /** Applies a fresher draft from the server when there are no local edits. */
  refresh: (draft: Draft) => void
  edit: (fn: (spec: OrchestrationSpec) => void) => void
  editAgent: (id: string, fn: (agent: AgentSpec) => void) => void
  /** Puts a library agent on the canvas. */
  addAgent: (record: AgentRecord, position: { x: number; y: number }) => void
  select: (s: Selection) => void
  setActiveAgent: (id: string | null) => void
  setProblems: (p: Problem[]) => void
  saveStarted: () => void
  agentSaved: (sent: AgentSpec, record: AgentRecord) => void
  graphSaved: (sent: string, version: number) => void
  saveSucceeded: (seq: number) => void
  saveFailed: (message: string) => void
  saveConflicted: (c: Conflict) => void
  loadTheirs: () => void
  keepMine: () => void
}

function loaded(draft: Draft) {
  const spec = expandDraft(draft)
  return {
    spec,
    version: draft.version,
    agentRecords: Object.fromEntries(draft.agents.map((a) => [a.id, a])),
    saved: {
      graph: canonical(toGraph(spec)),
      agents: Object.fromEntries(spec.agents.map((a) => [a.id, canonical(a)])),
    },
  }
}

function selectionExists(spec: OrchestrationSpec, s: Selection): boolean {
  if (s.kind === "agent") return spec.agents.some((a) => a.id === s.id)
  if (s.kind === "edge") return spec.edges.some((e) => e.from === s.from && e.to === s.to)
  return true
}

export const useEditor = create<EditorState>((set, get) => ({
  orchestrationId: null,
  spec: null,
  version: 0,
  agentRecords: {},
  saved: { graph: "", agents: {} },
  editSeq: 0,
  savedSeq: 0,
  saving: false,
  saveError: null,
  conflict: null,
  selection: { kind: "orchestration" },
  activeAgentId: null,
  problems: [],

  reset: (orchestrationId, draft) =>
    set({
      orchestrationId,
      ...loaded(draft),
      editSeq: 0,
      savedSeq: 0,
      saving: false,
      saveError: null,
      conflict: null,
      selection: { kind: "agent", id: draft.spec.rootAgent },
      activeAgentId: null,
      problems: [],
    }),

  refresh: (draft) => {
    const s = get()
    if (!s.spec || draft.orchestrationId !== s.orchestrationId || s.editSeq > s.savedSeq || s.saving || s.conflict) return
    const next = loaded(draft)
    const changed =
      draft.version !== s.version || draft.agents.some((a) => a.version !== s.agentRecords[a.id]?.version)
    if (!changed) {
      set({ agentRecords: next.agentRecords })
      return
    }
    set({ ...next, selection: selectionExists(next.spec, s.selection) ? s.selection : { kind: "orchestration" } })
  },

  edit: (fn) => {
    const spec = get().spec
    if (!spec) return
    const next = structuredClone(spec)
    fn(next)
    // A new edit also retries a failed save.
    set((s) => ({ spec: next, editSeq: s.editSeq + 1, saveError: null }))
  },

  editAgent: (id, fn) =>
    get().edit((spec) => {
      const a = spec.agents.find((x) => x.id === id)
      if (a) fn(a)
    }),

  addAgent: (record, position) => {
    const agent = agentSpec(record)
    set((s) => ({
      agentRecords: { ...s.agentRecords, [agent.id]: record },
      saved: { ...s.saved, agents: { ...s.saved.agents, [agent.id]: canonical(agent) } },
    }))
    get().edit((spec) => {
      spec.agents.push(agent)
      spec.layout[agent.id] = { x: Math.round(position.x), y: Math.round(position.y) }
    })
    set({ selection: { kind: "agent", id: agent.id } })
  },

  select: (selection) => set({ selection }),
  setActiveAgent: (activeAgentId) => set({ activeAgentId }),
  setProblems: (problems) => set({ problems }),
  saveStarted: () => set({ saving: true, saveError: null }),
  // Snapshots keep what was sent, not the server's echo, so normalization on the server never looks like an edit.
  agentSaved: (sent, record) =>
    set((s) => ({
      agentRecords: { ...s.agentRecords, [sent.id]: record },
      saved: { ...s.saved, agents: { ...s.saved.agents, [sent.id]: canonical(sent) } },
    })),
  graphSaved: (sent, version) => set((s) => ({ version, saved: { ...s.saved, graph: sent } })),
  saveSucceeded: (seq) => set({ saving: false, savedSeq: seq }),
  saveFailed: (saveError) => set({ saving: false, saveError }),
  saveConflicted: (conflict) => set({ saving: false, conflict }),

  // Replace the local copy with the other person's version.
  loadTheirs: () => {
    const { conflict: c, orchestrationId, spec } = get()
    if (!c || !orchestrationId || !spec) return
    if (c.kind === "draft") {
      get().reset(orchestrationId, c.draft)
      return
    }
    const theirs = agentSpec(c.agent)
    set((s) => ({
      conflict: null,
      spec: { ...spec, agents: spec.agents.map((a) => (a.id === theirs.id ? theirs : a)) },
      agentRecords: { ...s.agentRecords, [theirs.id]: c.agent },
      saved: { ...s.saved, agents: { ...s.saved.agents, [theirs.id]: canonical(theirs) } },
      // Other unsaved edits still need saving.
      editSeq: s.editSeq + 1,
    }))
  },

  // Overwrite the other person's change with the local copy on the next save.
  keepMine: () => {
    const c = get().conflict
    if (!c) return
    if (c.kind === "draft") set((s) => ({ conflict: null, version: c.draft.version, editSeq: s.editSeq + 1 }))
    else
      set((s) => ({
        conflict: null,
        agentRecords: { ...s.agentRecords, [c.agent.id]: c.agent },
        editSeq: s.editSeq + 1,
      }))
  },
}))

export function agentName(spec: OrchestrationSpec | null, id: string | null | undefined): string {
  return spec?.agents.find((a) => a.id === id)?.name ?? id ?? ""
}
