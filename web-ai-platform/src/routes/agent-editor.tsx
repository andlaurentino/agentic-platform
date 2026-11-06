import { useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate, useParams } from "@tanstack/react-router"
import { Trash2 } from "lucide-react"
import { useEffect, useState } from "react"
import { toast } from "sonner"
import { ApiError, api } from "@/api/client"
import { keys, saveAgent, useAgent, useDeleteAgent } from "@/api/hooks"
import type { AgentRecord, AgentSpec, Problem } from "@/api/types"
import { PageBody, PageHeader } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { AgentForm, ProblemList, Section } from "@/editor/agent-form"
import { agentSpec, canonical } from "@/editor/spec-utils"
import { getEditorName } from "@/lib/editor-name"

const VALIDATE_DELAY_MS = 500

export function AgentEditorPage() {
  const { id } = useParams({ strict: false }) as { id: string }
  const { data, isLoading, error } = useAgent(id)
  if (error) return <PageBody>{error.message}</PageBody>
  if (isLoading || !data)
    return (
      <>
        <PageHeader title="Agent" />
        <PageBody>
          <Skeleton className="h-64" />
        </PageBody>
      </>
    )
  // Remount the form whenever a new version is loaded.
  return <AgentPageForm key={`${data.id}-${data.version}`} record={data} />
}

function AgentPageForm({ record }: { record: AgentRecord }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const del = useDeleteAgent()
  const [agent, setAgent] = useState<AgentSpec>(() => agentSpec(record))
  const [saving, setSaving] = useState(false)
  const [problems, setProblems] = useState<Problem[]>([])
  const dirty = canonical(agent) !== canonical(agentSpec(record))

  useEffect(() => {
    const t = setTimeout(async () => {
      try {
        const res = await api<{ problems: Problem[] }>("/v1/agents/validate", { method: "POST", body: JSON.stringify({ agent }) })
        setProblems(res.problems)
      } catch {
        // validation is best effort
      }
    }, VALIDATE_DELAY_MS)
    return () => clearTimeout(t)
  }, [agent])

  const update = (fn: (a: AgentSpec) => void) =>
    setAgent((a) => {
      const next = structuredClone(a)
      fn(next)
      return next
    })

  const save = async () => {
    setSaving(true)
    try {
      const saved = await saveAgent(agent, record.version, getEditorName())
      qc.setQueryData(keys.agent(record.id), saved)
      void qc.invalidateQueries({ queryKey: keys.agents })
      toast.success(
        record.usedBy.length > 1 ? `Agent saved. ${record.usedBy.length} orchestrations use the new version.` : "Agent saved.",
      )
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        const theirs = (err.body as { agent: AgentRecord }).agent
        toast.error(err.message, {
          action: { label: "Load their version", onClick: () => qc.setQueryData(keys.agent(record.id), theirs) },
        })
      } else {
        toast.error((err as Error).message)
      }
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    try {
      await del.mutateAsync(record.id)
      toast.success("Agent deleted")
      navigate({ to: "/agents" })
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const inUse = record.usedBy.length > 0
  return (
    <>
      <PageHeader
        title={agent.name || "Untitled agent"}
        description={`Version ${record.version}${record.updatedByName ? ` · last edited by ${record.updatedByName}` : ""}`}
        actions={
          <>
            <Button
              variant="ghost"
              size="sm"
              onClick={remove}
              disabled={inUse || del.isPending}
              title={inUse ? "Remove the agent from every orchestration before deleting it" : undefined}
            >
              <Trash2 /> Delete
            </Button>
            <Button size="sm" onClick={save} disabled={!dirty || !agent.name.trim() || saving}>
              {saving ? "Saving…" : "Save"}
            </Button>
          </>
        }
      />
      <PageBody>
        <div className="mx-auto grid max-w-3xl gap-5">
          <Section title="Used in" hint={inUse ? "Saving changes the agent in all of these orchestrations." : undefined}>
            {!inUse && <p className="text-sm text-muted-foreground">No orchestration uses this agent yet. Add it from a canvas's right-click menu.</p>}
            {inUse && (
              <ul className="flex flex-wrap gap-2 text-sm">
                {record.usedBy.map((o) => (
                  <li key={o.id}>
                    <Button variant="outline" size="sm" asChild>
                      <Link to="/orchestrations/$id" params={{ id: o.id }} search={{ agent: record.id }}>
                        {o.name}
                      </Link>
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </Section>
          <ProblemList problems={problems} />
          <Separator />
          <AgentForm agent={agent} update={update} idPrefix={`agent-${record.id}`} />
        </div>
      </PageBody>
    </>
  )
}
