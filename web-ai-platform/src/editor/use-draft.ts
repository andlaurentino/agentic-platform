import { useQuery } from "@tanstack/react-query"
import { useEffect } from "react"
import { ApiError, api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Draft, Problem } from "@/api/types"
import { getEditorName } from "@/lib/editor-name"
import { useEditor } from "./store"

const SAVE_DELAY_MS = 800
const VALIDATE_DELAY_MS = 500

/** Loads the draft into the editor store, autosaves edits and validates them. */
export function useDraft(orchestrationId: string) {
  const query = useQuery({
    queryKey: keys.draft(orchestrationId),
    queryFn: () => api<Draft>(`/v1/orchestrations/${orchestrationId}/draft`),
    staleTime: Infinity,
    refetchOnMount: "always",
  })

  const loadedId = useEditor((s) => s.orchestrationId)
  useEffect(() => {
    if (query.data && loadedId !== orchestrationId) useEditor.getState().reset(orchestrationId, query.data)
  }, [query.data, loadedId, orchestrationId])

  // Autosave: when there are unsaved edits and no save in flight, save after a pause.
  const editSeq = useEditor((s) => s.editSeq)
  const savedSeq = useEditor((s) => s.savedSeq)
  const saving = useEditor((s) => s.saving)
  const conflict = useEditor((s) => s.conflict)
  const saveError = useEditor((s) => s.saveError)
  useEffect(() => {
    if (loadedId !== orchestrationId || editSeq <= savedSeq || saving || conflict || saveError) return
    const t = setTimeout(() => void saveDraft(orchestrationId), SAVE_DELAY_MS)
    return () => clearTimeout(t)
  }, [editSeq, savedSeq, saving, conflict, saveError, loadedId, orchestrationId])

  // Validate the local spec as it changes (advisory; runs also validate server-side).
  const spec = useEditor((s) => s.spec)
  useEffect(() => {
    if (!spec || loadedId !== orchestrationId) return
    const t = setTimeout(async () => {
      try {
        const res = await api<{ valid: boolean; problems: Problem[] }>(`/v1/orchestrations/${orchestrationId}/draft/validate`, {
          method: "POST",
          body: JSON.stringify({ spec }),
        })
        useEditor.getState().setProblems(res.problems)
      } catch {
        // validation is best effort
      }
    }, VALIDATE_DELAY_MS)
    return () => clearTimeout(t)
  }, [spec, loadedId, orchestrationId])

  // Warn before leaving with unsaved edits.
  useEffect(() => {
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      const s = useEditor.getState()
      if (s.editSeq > s.savedSeq) e.preventDefault()
    }
    window.addEventListener("beforeunload", onBeforeUnload)
    return () => window.removeEventListener("beforeunload", onBeforeUnload)
  }, [])

  return query
}

export async function saveDraft(orchestrationId: string) {
  const s = useEditor.getState()
  if (!s.spec) return
  const seq = s.editSeq
  s.saveStarted()
  try {
    const draft = await api<Draft>(`/v1/orchestrations/${orchestrationId}/draft`, {
      method: "PUT",
      headers: { "If-Match": `"${s.version}"` },
      body: JSON.stringify({ spec: s.spec, updatedByName: getEditorName() }),
    })
    useEditor.getState().saveSucceeded(seq, draft)
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      const body = err.body as { draft: Draft; error: { message: string } }
      useEditor.getState().saveConflicted(body.draft, body.error.message)
    } else {
      useEditor.getState().saveFailed((err as Error).message)
    }
  }
}
