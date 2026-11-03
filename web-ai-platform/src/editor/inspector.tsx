import { Link } from "@tanstack/react-router"
import { AlertCircle, AlertTriangle, Crown, Trash2, X } from "lucide-react"
import { useMemo, type ReactNode } from "react"
import { useCatalog, useMcpTools, useSkills } from "@/api/hooks"
import type { AgentSpec, AssetRef, Config, EdgeMode, Manifest, OrchestrationSpec, Problem } from "@/api/types"
import { SchemaForm } from "@/components/schema-form"
import { hasFields } from "@/lib/schema"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"
import { useEditor } from "./store"

function Section({ title, hint, children }: { title: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <section className="grid gap-2">
      <div>
        <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{title}</h3>
        {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      </div>
      {children}
    </section>
  )
}

export function ProblemList({ problems, onSelect }: { problems: Problem[]; onSelect?: (agentId: string) => void }) {
  if (problems.length === 0) return null
  return (
    <ul className="grid gap-1.5">
      {problems.map((p, i) => (
        <li key={i}>
          <button
            type="button"
            className="flex w-full items-start gap-2 rounded-md p-1.5 text-left text-xs hover:bg-muted disabled:hover:bg-transparent"
            onClick={() => p.agentId && onSelect?.(p.agentId)}
            disabled={!onSelect || !p.agentId}
          >
            {p.severity === "error" ? (
              <AlertCircle className="mt-0.5 size-3.5 shrink-0 text-destructive" />
            ) : (
              <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" />
            )}
            <span>{p.message}</span>
          </button>
        </li>
      ))}
    </ul>
  )
}

/** Checkbox list of guardrail assets with per-guardrail config. */
function GuardrailPicker({ value, onChange, idPrefix }: { value: AssetRef[]; onChange: (v: AssetRef[]) => void; idPrefix: string }) {
  const { data: guardrails } = useCatalog("guardrail")
  return (
    <div className="grid gap-3">
      {guardrails?.map((g) => {
        const ref = value.find((r) => r.asset === g.id)
        return (
          <div key={g.id} className="grid gap-2 rounded-md border p-2.5">
            <label className="flex items-start gap-2 text-sm">
              <Checkbox
                className="mt-0.5"
                checked={!!ref}
                onCheckedChange={(c) => onChange(c ? [...value, { asset: g.id }] : value.filter((r) => r.asset !== g.id))}
              />
              <span className="grid gap-0.5">
                <span className="flex items-center gap-1.5 font-medium">
                  {g.name} {g.extra?.level && <Badge variant="outline">{g.extra.level}</Badge>}
                </span>
                <span className="text-xs text-muted-foreground">{g.description}</span>
              </span>
            </label>
            {ref && hasFields(g.configSchema) && (
              <div className="pl-6">
                <SchemaForm
                  idPrefix={`${idPrefix}-${g.id}`}
                  schema={g.configSchema}
                  value={ref.config}
                  onChange={(config) => onChange(value.map((r) => (r.asset === g.id ? { ...r, config } : r)))}
                />
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

function McpToolPicker({ mcp, agent }: { mcp: Manifest; agent: AgentSpec }) {
  const { data: tools, error, isLoading } = useMcpTools(mcp.id)
  const { editAgent } = useEditor.getState()
  const selected = agent.tools.find((t) => t.mcp === mcp.id)?.tools ?? []

  const toggle = (name: string, on: boolean) =>
    editAgent(agent.id, (a) => {
      const next = on ? [...selected, name] : selected.filter((n) => n !== name)
      a.tools = a.tools.filter((t) => t.mcp !== mcp.id)
      if (next.length > 0) a.tools.push({ mcp: mcp.id, tools: next })
    })

  return (
    <div className="grid gap-2 rounded-md border p-2.5">
      <p className="text-sm font-medium">{mcp.name}</p>
      {isLoading && <p className="text-xs text-muted-foreground">Loading tools…</p>}
      {error && <p className="text-xs text-destructive">Server unreachable: {error.message}</p>}
      {tools?.map((t) => (
        <label key={t.name} className="flex items-start gap-2 text-sm">
          <Checkbox className="mt-0.5" checked={selected.includes(t.name)} onCheckedChange={(c) => toggle(t.name, !!c)} />
          <span className="grid">
            <code className="font-mono text-xs font-medium">{t.name}</code>
            <span className="text-xs text-muted-foreground">{t.description}</span>
          </span>
        </label>
      ))}
    </div>
  )
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
          <Select
            value={e.mode}
            onValueChange={(mode) =>
              edit((s) => {
                const x = s.edges.find((y) => y.from === e.from && y.to === e.to)
                if (x) x.mode = mode as EdgeMode
              })
            }
          >
            <SelectTrigger className="h-8 w-36 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="transfer">Hands off</SelectItem>
              <SelectItem value="tool">Calls as tool</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="ghost"
            size="icon"
            className="size-8"
            aria-label={`Stop delegating to ${name(e.to)}`}
            onClick={() =>
              edit((s) => {
                s.edges = s.edges.filter((y) => !(y.from === e.from && y.to === e.to))
              })
            }
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
  // Select the stable array, then filter: a selector returning a new array re-renders forever.
  const allProblems = useEditor((s) => s.problems)
  const problems = useMemo(() => allProblems.filter((p) => p.agentId === agentId), [allProblems, agentId])
  const { editAgent, edit, select } = useEditor.getState()
  const { data: llms } = useCatalog("llm")
  const { data: tools } = useCatalog("tool")
  const { data: mcps } = useCatalog("mcp")
  const { data: skills } = useSkills()

  const agent = spec.agents.find((a) => a.id === agentId)
  if (!agent) return <p className="p-4 text-sm text-muted-foreground">Select an agent on the canvas.</p>
  const isRoot = spec.rootAgent === agent.id
  const llm = llms?.find((m) => m.id === agent.model.asset)
  const setField = <K extends keyof AgentSpec>(key: K, v: AgentSpec[K]) => editAgent(agent.id, (a) => void (a[key] = v))

  return (
    <div className="grid gap-5 p-4">
      <div className="flex items-center gap-2">
        {isRoot && (
          <Badge variant="secondary" className="gap-1">
            <Crown className="size-3 text-amber-500" /> Root agent
          </Badge>
        )}
        <div className="flex-1" />
        {!isRoot && (
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                edit((s) => {
                  s.rootAgent = agent.id
                  // The root cannot have a parent.
                  s.edges = s.edges.filter((e) => e.to !== agent.id)
                })
              }
            >
              <Crown /> Make root
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                edit((s) => {
                  s.agents = s.agents.filter((a) => a.id !== agent.id)
                  s.edges = s.edges.filter((e) => e.from !== agent.id && e.to !== agent.id)
                  delete s.layout[agent.id]
                })
                select({ kind: "orchestration" })
              }}
            >
              <Trash2 /> Delete
            </Button>
          </>
        )}
      </div>

      <ProblemList problems={problems} />

      <Section title="Agent">
        <div className="grid gap-1.5">
          <Label htmlFor="agent-name">Name</Label>
          <Input id="agent-name" value={agent.name} onChange={(e) => setField("name", e.target.value)} />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="agent-desc">Description</Label>
          <Input
            id="agent-desc"
            value={agent.description}
            onChange={(e) => setField("description", e.target.value)}
            placeholder="When should other agents hand off to this one?"
          />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="agent-instructions">Instructions</Label>
          <Textarea
            id="agent-instructions"
            className="min-h-32 text-sm"
            value={agent.instructions}
            onChange={(e) => setField("instructions", e.target.value)}
            placeholder="You are a helpful assistant that…"
          />
        </div>
      </Section>

      <Separator />
      <Section title="Model">
        <Select value={agent.model.asset} onValueChange={(asset) => editAgent(agent.id, (a) => void (a.model = { asset }))}>
          <SelectTrigger className="w-full">
            <SelectValue placeholder="Choose a model" />
          </SelectTrigger>
          <SelectContent>
            {llms?.map((m) => (
              <SelectItem key={m.id} value={m.id}>
                {m.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {llm && (
          <SchemaForm
            idPrefix={`${agent.id}-model`}
            schema={llm.configSchema}
            value={agent.model.config}
            onChange={(config: Config) => editAgent(agent.id, (a) => void (a.model.config = config))}
          />
        )}
      </Section>

      <Separator />
      <Section
        title="Skills"
        hint={
          <>
            Added to this agent's instructions.{" "}
            <Link to="/skills" className="underline">
              Manage skills
            </Link>
          </>
        }
      >
        {skills?.length === 0 && <p className="text-xs text-muted-foreground">No skills yet.</p>}
        {skills?.map((s) => (
          <label key={s.id} className="flex items-start gap-2 text-sm">
            <Checkbox
              className="mt-0.5"
              checked={agent.skills.includes(s.id)}
              onCheckedChange={(c) => setField("skills", c ? [...agent.skills, s.id] : agent.skills.filter((x) => x !== s.id))}
            />
            <span className="grid">
              <span className="font-medium">{s.name}</span>
              {s.description && <span className="text-xs text-muted-foreground">{s.description}</span>}
            </span>
          </label>
        ))}
      </Section>

      <Separator />
      <Section title="Tools">
        {tools?.map((t) => {
          const ref = agent.tools.find((x) => x.asset === t.id)
          return (
            <div key={t.id} className="grid gap-2">
              <label className="flex items-start gap-2 text-sm">
                <Checkbox
                  className="mt-0.5"
                  checked={!!ref}
                  onCheckedChange={(c) =>
                    editAgent(agent.id, (a) => {
                      a.tools = c ? [...a.tools, { asset: t.id }] : a.tools.filter((x) => x.asset !== t.id)
                    })
                  }
                />
                <span className="grid">
                  <span className="font-medium">{t.name}</span>
                  <span className="text-xs text-muted-foreground">{t.description}</span>
                </span>
              </label>
              {ref && hasFields(t.configSchema) && (
                <div className="pl-6">
                  <SchemaForm
                    idPrefix={`${agent.id}-${t.id}`}
                    schema={t.configSchema}
                    value={ref.config}
                    onChange={(config) =>
                      editAgent(agent.id, (a) => {
                        const x = a.tools.find((y) => y.asset === t.id)
                        if (x) x.config = config
                      })
                    }
                  />
                </div>
              )}
            </div>
          )
        })}
      </Section>

      <Separator />
      <Section title="MCP tools" hint="Only tools in each server's allow-list are shown.">
        {mcps?.map((m) => <McpToolPicker key={m.id} mcp={m} agent={agent} />)}
      </Section>

      <Separator />
      <Section title="Guardrails" hint="Only for this agent. Guardrails for every agent are in Settings.">
        <GuardrailPicker idPrefix={agent.id} value={agent.guardrails} onChange={(g) => setField("guardrails", g)} />
      </Section>

      <Separator />
      <Section title="Delegates to" hint="Hand off passes the conversation over. Calls as tool asks the agent and gets its answer back.">
        <Delegation agent={agent} spec={spec} />
      </Section>
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
          onValueChange={(id) =>
            edit((s) => {
              s.rootAgent = id
              s.edges = s.edges.filter((e) => e.to !== id)
            })
          }
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
