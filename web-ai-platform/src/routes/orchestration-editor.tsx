import { ReactFlowProvider } from "@xyflow/react"
import { useParams, useSearch } from "@tanstack/react-router"
import { AlertCircle, Check, CloudOff, Loader2 } from "lucide-react"
import { useEffect, useState } from "react"
import { useOrchestration } from "@/api/hooks"
import { PageHeader } from "@/components/page"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Canvas } from "@/editor/canvas"
import { AgentInspector, EdgeInspector, OrchestrationSettings } from "@/editor/inspector"
import { useEditor } from "@/editor/store"
import { saveDraft, useDraft } from "@/editor/use-draft"
import { Playground } from "@/playground/playground"

function SaveStatus({ orchestrationId }: { orchestrationId: string }) {
  const saving = useEditor((s) => s.saving)
  const unsaved = useEditor((s) => s.editSeq > s.savedSeq)
  const version = useEditor((s) => s.version)
  const saveError = useEditor((s) => s.saveError)
  const conflict = useEditor((s) => s.conflict)
  if (conflict)
    return (
      <span className="flex items-center gap-1.5 text-xs text-destructive">
        <CloudOff className="size-3.5" /> Not saved: conflict
      </span>
    )
  if (saveError)
    return (
      <span className="flex items-center gap-1.5 text-xs text-destructive">
        <CloudOff className="size-3.5" /> Not saved: {saveError}
        <Button variant="link" size="sm" className="h-auto p-0 text-xs" onClick={() => void saveDraft(orchestrationId)}>
          Retry
        </Button>
      </span>
    )
  if (saving || unsaved)
    return (
      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" /> Saving…
      </span>
    )
  return (
    <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
      <Check className="size-3.5" /> Saved · draft v{version}
    </span>
  )
}

function ConflictBanner() {
  const conflict = useEditor((s) => s.conflict)
  const orchestrationId = useEditor((s) => s.orchestrationId)
  if (!conflict || !orchestrationId) return null
  return (
    <Alert variant="destructive" className="m-3 mb-0 w-auto">
      <AlertCircle />
      <AlertTitle>{conflict.kind === "agent" ? "Someone else changed this agent" : "Someone else changed this orchestration"}</AlertTitle>
      <AlertDescription className="flex flex-wrap items-center gap-2">
        <span>{conflict.message}. Your latest edits are not saved.</span>
        <Button size="sm" variant="outline" onClick={() => useEditor.getState().loadTheirs()}>
          Load their version
        </Button>
        <Button size="sm" variant="ghost" onClick={() => useEditor.getState().keepMine()}>
          Keep mine
        </Button>
      </AlertDescription>
    </Alert>
  )
}

export function OrchestrationEditorPage() {
  const { id } = useParams({ strict: false }) as { id: string }
  const search = useSearch({ strict: false }) as { agent?: string }
  const orchestration = useOrchestration(id)
  const draft = useDraft(id)
  const ready = useEditor((s) => s.orchestrationId === id && s.spec !== null)
  const selection = useEditor((s) => s.selection)
  const errorCount = useEditor((s) => s.problems.filter((p) => p.severity === "error").length)
  const [tab, setTab] = useState("agent")

  // Follow the canvas selection unless the user is chatting.
  useEffect(
    () =>
      useEditor.subscribe((s, prev) => {
        if (s.selection !== prev.selection) setTab((t) => (t === "test" ? t : s.selection.kind === "orchestration" ? "settings" : "agent"))
      }),
    [],
  )

  // Deep link from the Agents page.
  useEffect(() => {
    if (ready && search.agent) useEditor.getState().select({ kind: "agent", id: search.agent })
  }, [ready, search.agent])

  if (draft.error || orchestration.error)
    return <p className="p-6 text-sm text-destructive">{(draft.error ?? orchestration.error)?.message}</p>

  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title={orchestration.data?.name ?? "Orchestration"}
        description={orchestration.data?.description}
        actions={
          ready && (
            <>
              {errorCount > 0 && (
                <Badge
                  variant="destructive"
                  className="cursor-pointer"
                  onClick={() => {
                    useEditor.getState().select({ kind: "orchestration" })
                    setTab("settings")
                  }}
                >
                  {errorCount} {errorCount === 1 ? "problem" : "problems"}
                </Badge>
              )}
              <SaveStatus orchestrationId={id} />
            </>
          )
        }
      />
      <ConflictBanner />
      {!ready ? (
        <Skeleton className="m-4 flex-1" />
      ) : (
        <div className="flex min-h-0 flex-1">
          <div className="min-w-0 flex-1">
            <ReactFlowProvider>
              <Canvas />
            </ReactFlowProvider>
          </div>
          <aside className="flex w-[420px] shrink-0 flex-col border-l">
            <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-0">
              <TabsList className="m-2 w-auto">
                <TabsTrigger value="agent">{selection.kind === "edge" ? "Connection" : "Agent"}</TabsTrigger>
                <TabsTrigger value="settings">Settings</TabsTrigger>
                <TabsTrigger value="test">Test</TabsTrigger>
              </TabsList>
              <TabsContent value="agent" className="min-h-0 flex-1 overflow-y-auto">
                {selection.kind === "agent" ? (
                  <AgentInspector agentId={selection.id} />
                ) : selection.kind === "edge" ? (
                  <EdgeInspector from={selection.from} to={selection.to} />
                ) : (
                  <p className="p-4 text-sm text-muted-foreground">Select an agent on the canvas to edit it.</p>
                )}
              </TabsContent>
              <TabsContent value="settings" className="min-h-0 flex-1 overflow-y-auto">
                <OrchestrationSettings />
              </TabsContent>
              {/* Keep the playground mounted so the chat survives tab switches. */}
              <TabsContent value="test" forceMount className="min-h-0 flex-1 data-[state=inactive]:hidden">
                <Playground orchestrationId={id} />
              </TabsContent>
            </Tabs>
          </aside>
        </div>
      )}
    </div>
  )
}
