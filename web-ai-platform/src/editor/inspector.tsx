import { Link } from "@tanstack/react-router"
import { ArrowDown, Crown, ExternalLink, Trash2, X } from "lucide-react"
import { useMemo } from "react"
import type { AgentSpec, EdgeMode, OrchestrationSpec } from "@/api/types"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { AgentForm, GuardrailPicker, ProblemList, Section } from "./agent-form"
import { makeRoot, removeAgent, removeEdge } from "./spec-utils"
import { useEditor } from "./store"

export { ProblemList } from "./agent-form"

export const edgeModeHint = "Hand off passes the conversation over. Calls as tool asks the agent and gets its answer back."

export function EdgeModeSelect({ value, onChange, className }: { value: EdgeMode; onChange: (m: EdgeMode) => void; className?: string }) {
  return (
    <Select value={value} onValueChange={(m) => onChange(m as EdgeMode)}>
      <SelectTrigger className={className}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="transfer">Hands off</SelectItem>
        <SelectItem value="tool">Calls as tool</SelectItem>
      </SelectContent>
    </Select>
  )
}

function setEdgeMode(from: string, to: string, mode: EdgeMode) {
  useEditor.getState().edit((s) => {
    const x = s.edges.find((y) => y.from === from && y.to === to)
    if (x) x.mode = mode
  })
}

function Delegation({ agent, spec }: { agent: AgentSpec; spec: OrchestrationSpec }) {
  const { edit } = useEditor.getState()
  const outgoing = spec.edges.filter((e) => e.from === agent.id)
  const candidates = spec.agents.filter(
    (a) => a.id !== agent.id && a.id !== spec.rootAgent && !outgoing.some((e) => e.to === a.id),
  )
  const name = (id: string) => spec.agents.find((a) => a.id === id)?.name ?? id

  return (
    <div className="grid gap-2">
      {outgoing.length === 0 && <p className="text-xs text-muted-foreground">This agent answers on its own.</p>}
      {outgoing.map((e) => (
        <div key={e.to} className="flex items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-sm">{name(e.to)}</span>
          <EdgeModeSelect value={e.mode} onChange={(m) => setEdgeMode(e.from, e.to, m)} className="h-8 w-36 text-xs" />
          <Button
            variant="ghost"
            size="icon"
            className="size-8"
            aria-label={`Stop delegating to ${name(e.to)}`}
            onClick={() => edit((s) => removeEdge(s, e.from, e.to))}
          >
            <X />
          </Button>
        </div>
      ))}
      {candidates.length > 0 && (
        <Select
          value=""
          onValueChange={(to) =>
            edit((s) => {
              s.edges.push({ from: agent.id, to, mode: "transfer" })
            })
          }
        >
          <SelectTrigger className="h-8 text-xs">
            <SelectValue placeholder="Delegate to another agent…" />
          </SelectTrigger>
          <SelectContent>
            {candidates.map((a) => (
              <SelectItem key={a.id} value={a.id}>
                {a.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </div>
  )
}

export function AgentInspector({ agentId }: { agentId: string }) {
  const spec = useEditor((s) => s.spec)!
  const record = useEditor((s) => s.agentRecords[agentId])
  const orchestrationId = useEditor((s) => s.orchestrationId)
  // Select the stable array, then filter: a selector returning a new array re-renders forever.
  const allProblems = useEditor((s) => s.problems)
  const problems = useMemo(() => allProblems.filter((p) => p.agentId === agentId), [allProblems, agentId])
  const { editAgent, edit, select } = useEditor.getState()

  const agent = spec.agents.find((a) => a.id === agentId)
  if (!agent) return <p className="p-4 text-sm text-muted-foreground">Select an agent on the canvas.</p>
  const isRoot = spec.rootAgent === agent.id
  const elsewhere = (record?.usedBy ?? []).filter((o) => o.id !== orchestrationId)

  return (
    <div className="grid gap-5 p-4">
      <div className="flex flex-wrap items-center gap-2">
        {isRoot && (
          <Badge variant="secondary" className="gap-1">
            <Crown className="size-3 text-amber-500" /> Root agent
          </Badge>
        )}
        <div className="flex-1" />
        <Button variant="ghost" size="sm" asChild>
          <Link to="/agents/$id" params={{ id: agent.id }}>
            <ExternalLink /> Agent page
          </Link>
        </Button>
        {!isRoot && (
          <>
            <Button variant="outline" size="sm" onClick={() => edit((s) => makeRoot(s, agent.id))}>
              <Crown /> Make root
            </Button>
            <Button
              variant="ghost"
              size="sm"
              title="Removes the agent from this orchestration. It stays in the agent library."
              onClick={() => {
                edit((s) => removeAgent(s, agent.id))
                select({ kind: "orchestration" })
              }}
            >
              <Trash2 /> Remove
            </Button>
          </>
        )}
      </div>

      {elsewhere.length > 0 && (
        <p className="rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">
          Shared agent: changes also apply to {elsewhere.map((o) => o.name).join(", ")}.
        </p>
      )}

      <ProblemList problems={problems} />

      <AgentForm agent={agent} idPrefix={agent.id} update={(fn: (a: AgentSpec) => void) => editAgent(agent.id, fn)} />

      <Separator />
      <Section title="Delegates to" hint={edgeModeHint}>
        <Delegation agent={agent} spec={spec} />
      </Section>
    </div>
  )
}

export function EdgeInspector({ from, to }: { from: string; to: string }) {
  const spec = useEditor((s) => s.spec)!
  const allProblems = useEditor((s) => s.problems)
  const { edit, select } = useEditor.getState()
  const index = spec.edges.findIndex((e) => e.from === from && e.to === to)
  const problems = useMemo(
    () => allProblems.filter((p) => p.path === `edges[${index}]` || p.path.startsWith(`edges[${index}].`)),
    [allProblems, index],
  )
  const edge = spec.edges[index]
  if (!edge) return <p className="p-4 text-sm text-muted-foreground">Select a connection on the canvas.</p>
  const name = (id: string) => spec.agents.find((a) => a.id === id)?.name || "Untitled agent"
  const agentButton = (id: string) => (
    <Button variant="outline" className="w-full justify-start" onClick={() => select({ kind: "agent", id })}>
      {id === spec.rootAgent && <Crown className="text-amber-500" />}
      <span className="truncate">{name(id)}</span>
    </Button>
  )

  return (
    <div className="grid gap-5 p-4">
      <ProblemList problems={problems} />
      <Section title="Connection">
        <div className="grid justify-items-center gap-1">
          {agentButton(edge.from)}
          <ArrowDown className="size-4 text-muted-foreground" />
          {agentButton(edge.to)}
        </div>
      </Section>
      <Separator />
      <Section title="How it delegates" hint={edgeModeHint}>
        <EdgeModeSelect value={edge.mode} onChange={(m) => setEdgeMode(from, to, m)} className="w-full" />
      </Section>
      <Separator />
      <Button
        variant="outline"
        className="justify-self-start"
        onClick={() => {
          edit((s) => removeEdge(s, from, to))
          select({ kind: "orchestration" })
        }}
      >
        <X /> Remove connection
      </Button>
    </div>
  )
}

export function OrchestrationSettings() {
  const spec = useEditor((s) => s.spec)!
  const problems = useEditor((s) => s.problems)
  const { edit, select } = useEditor.getState()

  return (
    <div className="grid gap-5 p-4">
      <Section title="Problems" hint={problems.length === 0 ? "None. The orchestration is ready to run." : undefined}>
        <ProblemList problems={problems} onSelect={(id) => select({ kind: "agent", id })} />
      </Section>
      <Separator />
      <Section title="Root agent" hint="Every conversation starts here.">
        <Select
          value={spec.rootAgent}
          onValueChange={(id) => edit((s) => makeRoot(s, id))}
        >
          <SelectTrigger className="w-full">
            <SelectValue placeholder="Choose the root agent" />
          </SelectTrigger>
          <SelectContent>
            {spec.agents.map((a) => (
              <SelectItem key={a.id} value={a.id}>
                {a.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Section>
      <Separator />
      <Section title="Guardrails for every agent">
        <GuardrailPicker
          idPrefix="orchestration"
          value={spec.guardrails}
          onChange={(g) =>
            edit((s) => {
              s.guardrails = g
            })
          }
        />
      </Section>
    </div>
  )
}
