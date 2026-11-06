import { Link } from "@tanstack/react-router"
import { AlertCircle, AlertTriangle } from "lucide-react"
import type { ReactNode } from "react"
import { useCatalog, useMcpTools, useSkills } from "@/api/hooks"
import type { AgentSpec, AssetRef, Config, Manifest, Problem } from "@/api/types"
import { SchemaForm } from "@/components/schema-form"
import { hasFields } from "@/lib/schema"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"

/** Applies a change to the agent being edited. */
export type UpdateAgent = (fn: (agent: AgentSpec) => void) => void

export function Section({ title, hint, children }: { title: string; hint?: ReactNode; children: ReactNode }) {
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
export function GuardrailPicker({ value, onChange, idPrefix }: { value: AssetRef[]; onChange: (v: AssetRef[]) => void; idPrefix: string }) {
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

function McpToolPicker({ mcp, agent, update }: { mcp: Manifest; agent: AgentSpec; update: UpdateAgent }) {
  const { data: tools, error, isLoading } = useMcpTools(mcp.id)
  const selected = agent.tools.find((t) => t.mcp === mcp.id)?.tools ?? []

  const toggle = (name: string, on: boolean) =>
    update((a) => {
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

/** The fields of a library agent, shared by the orchestration inspector and the agent page. */
export function AgentForm({ agent, update, idPrefix }: { agent: AgentSpec; update: UpdateAgent; idPrefix: string }) {
  const { data: llms } = useCatalog("llm")
  const { data: tools } = useCatalog("tool")
  const { data: mcps } = useCatalog("mcp")
  const { data: skills } = useSkills()
  const llm = llms?.find((m) => m.id === agent.model.asset)
  const setField = <K extends keyof AgentSpec>(key: K, v: AgentSpec[K]) => update((a) => void (a[key] = v))

  return (
    <>
      <Section title="Agent">
        <div className="grid gap-1.5">
          <Label htmlFor={`${idPrefix}-name`}>Name</Label>
          <Input id={`${idPrefix}-name`} value={agent.name} onChange={(e) => setField("name", e.target.value)} />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor={`${idPrefix}-desc`}>Description</Label>
          <Input
            id={`${idPrefix}-desc`}
            value={agent.description}
            onChange={(e) => setField("description", e.target.value)}
            placeholder="When should other agents hand off to this one?"
          />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor={`${idPrefix}-instructions`}>Instructions</Label>
          <Textarea
            id={`${idPrefix}-instructions`}
            className="min-h-32 text-sm"
            value={agent.instructions}
            onChange={(e) => setField("instructions", e.target.value)}
            placeholder="You are a helpful assistant that…"
          />
        </div>
      </Section>

      <Separator />
      <Section title="Model">
        {/* The provider (LLM gateway) picker only matters when more than one is registered. */}
        {(!llm || (llms?.length ?? 0) > 1) && (
          <Select
            value={agent.model.asset}
            onValueChange={(asset) => {
              // Switching provider resets settings and picks that provider's default model.
              const def = llms?.find((m) => m.id === asset)?.extra?.models?.find((m) => m.default)?.id
              update((a) => void (a.model = def ? { asset, config: { model: def } } : { asset }))
            }}
          >
            <SelectTrigger className="w-full">
              <SelectValue placeholder="Choose an LLM provider" />
            </SelectTrigger>
            <SelectContent>
              {llms?.map((m) => (
                <SelectItem key={m.id} value={m.id}>
                  {m.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {llm?.extra?.status?.status === "failed" && (
          <p className="text-xs text-destructive">
            {llm.name} is unavailable: {llm.extra.status.error}
          </p>
        )}
        {llm && llm.extra?.credentialsConfigured === false && (
          <p className="text-xs text-destructive">
            {llm.extra.apiKeyEnv} is not set on the server, so runs with this provider will fail.
          </p>
        )}
        {llm && (
          <SchemaForm
            idPrefix={`${idPrefix}-model`}
            schema={llm.configSchema}
            value={agent.model.config}
            onChange={(config: Config) => update((a) => void (a.model.config = config))}
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
                    update((a) => {
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
                    idPrefix={`${idPrefix}-${t.id}`}
                    schema={t.configSchema}
                    value={ref.config}
                    onChange={(config) =>
                      update((a) => {
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
        {mcps?.map((m) => <McpToolPicker key={m.id} mcp={m} agent={agent} update={update} />)}
      </Section>

      <Separator />
      <Section title="Guardrails" hint="Only for this agent. Orchestrations can add guardrails for every agent in their settings.">
        <GuardrailPicker idPrefix={idPrefix} value={agent.guardrails} onChange={(g) => setField("guardrails", g)} />
      </Section>
    </>
  )
}
