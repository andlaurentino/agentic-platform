import { create } from "zustand"
import type { AgentSpec, Draft, OrchestrationSpec, Problem } from "@/api/types"

export type Selection = { kind: "agent"; id: string } | { kind: "orchestration" }

interface EditorState {
  orchestrationId: string | null
  spec: OrchestrationSpec | null
  version: number
  /** Incremented on every local edit; compared with savedSeq to know what is unsaved. */
  editSeq: number
  savedSeq: number
  saving: boolean
  saveError: string | null
  conflict: { draft: Draft; message: string } | null
  selection: Selection
  /** Agent currently speaking in the playground, highlighted on the canvas. */
  activeAgentId: string | null
  problems: Problem[]

  reset: (orchestrationId: string, draft: Draft) => void
  edit: (fn: (spec: OrchestrationSpec) => void) => void
  editAgent: (id: string, fn: (agent: AgentSpec) => void) => void
  select: (s: Selection) => void
  setActiveAgent: (id: string | null) => void
  setProblems: (p: Problem[]) => void
  saveStarted: () => void
  saveSucceeded: (seq: number, draft: Draft) => void
  saveFailed: (message: string) => void
  saveConflicted: (draft: Draft, message: string) => void
  keepMine: () => void
}

export const useEditor = create<EditorState>((set, get) => ({
  orchestrationId: null,
  spec: null,
  version: 0,
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
      spec: draft.spec,
      version: draft.version,
      editSeq: 0,
      savedSeq: 0,
      saving: false,
      saveError: null,
      conflict: null,
      selection: { kind: "agent", id: draft.spec.rootAgent },
      activeAgentId: null,
      problems: [],
    }),

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

  select: (selection) => set({ selection }),
  setActiveAgent: (activeAgentId) => set({ activeAgentId }),
  setProblems: (problems) => set({ problems }),
  saveStarted: () => set({ saving: true, saveError: null }),
  saveSucceeded: (seq, draft) => set({ saving: false, savedSeq: seq, version: draft.version }),
  saveFailed: (saveError) => set({ saving: false, saveError }),
  saveConflicted: (draft, message) => set({ saving: false, conflict: { draft, message } }),
  // Overwrite the other person's change with the local spec on the next save.
  keepMine: () => {
    const c = get().conflict
    if (c) set((s) => ({ conflict: null, version: c.draft.version, editSeq: s.editSeq + 1 }))
  },
}))

export function agentName(spec: OrchestrationSpec | null, id: string | null | undefined): string {
  return spec?.agents.find((a) => a.id === id)?.name ?? id ?? ""
}
