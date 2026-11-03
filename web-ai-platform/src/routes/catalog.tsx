import { useParams } from "@tanstack/react-router"
import { AlertCircle, KeyRound } from "lucide-react"
import { useCatalog, useMcpTools } from "@/api/hooks"
import type { Kind, Manifest } from "@/api/types"
import { EmptyState, PageBody, PageHeader } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"

const titles: Record<Kind, { title: string; description: string }> = {
  tool: { title: "Tools", description: "Native functions engineers registered in agent-assets." },
  mcp: { title: "MCP servers", description: "MCP server contracts and the tools agents are allowed to use." },
  guardrail: { title: "Guardrails", description: "Policy checks that run around model calls. L1 is rule-based, L2 is model-based." },
  llm: { title: "LLMs", description: "Model connectors available to agents." },
}

export function LifecycleBadge({ lifecycle }: { lifecycle: Manifest["lifecycle"] }) {
  const variant = lifecycle === "stable" ? "secondary" : lifecycle === "deprecated" ? "destructive" : "outline"
  return <Badge variant={variant}>{lifecycle}</Badge>
}

function McpTools({ id }: { id: string }) {
  const { data, error, isLoading } = useMcpTools(id)
  if (isLoading) return <Skeleton className="h-16" />
  if (error)
    return (
      <p className="flex items-start gap-2 text-xs text-destructive">
        <AlertCircle className="mt-0.5 size-3.5 shrink-0" /> Server unreachable: {error.message}
      </p>
    )
  return (
    <ul className="grid gap-1.5">
      {data?.map((t) => (
        <li key={t.name} className="text-xs">
          <code className="font-mono font-medium">{t.name}</code>
          <span className="text-muted-foreground"> — {t.description}</span>
        </li>
      ))}
    </ul>
  )
}

function AssetCard({ m }: { m: Manifest }) {
  const fields = Object.entries(m.configSchema.properties ?? {})
  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-2">
          <CardTitle className="text-base">{m.name}</CardTitle>
          <div className="flex gap-1">
            {m.extra?.level && <Badge variant="outline">{m.extra.level}</Badge>}
            <LifecycleBadge lifecycle={m.lifecycle} />
          </div>
        </div>
        <CardDescription>{m.description}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 text-sm">
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
          <dt className="text-muted-foreground">ID</dt>
          <dd className="font-mono">{m.id}</dd>
          <dt className="text-muted-foreground">Owner</dt>
          <dd>{m.owner}</dd>
          {m.extra?.stages && (
            <>
              <dt className="text-muted-foreground">Runs on</dt>
              <dd>{m.extra.stages.join(", ")}</dd>
            </>
          )}
          {m.extra?.transport && (
            <>
              <dt className="text-muted-foreground">Transport</dt>
              <dd>
                {m.extra.transport} · auth {m.extra.auth}
              </dd>
            </>
          )}
        </dl>
        {m.secrets && m.secrets.length > 0 && (
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <KeyRound className="size-3.5" /> Needs secret {m.secrets.map((s) => s.name).join(", ")}
          </p>
        )}
        {fields.length > 0 && (
          <div>
            <p className="mb-1 text-xs font-medium">Settings builders can change</p>
            <ul className="grid gap-0.5 text-xs text-muted-foreground">
              {fields.map(([k, p]) => (
                <li key={k}>
                  {p.title ?? k}
                  {p.enum && `: ${p.enum.join(" / ")}`}
                  {p.default !== undefined && !Array.isArray(p.default) && ` (default ${String(p.default)})`}
                </li>
              ))}
            </ul>
          </div>
        )}
        {m.kind === "mcp" && (
          <div>
            <p className="mb-1 text-xs font-medium">Allowed tools</p>
            <McpTools id={m.id} />
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export function CatalogPage() {
  const { kind } = useParams({ strict: false }) as { kind: Kind }
  const { data, isLoading, error } = useCatalog(kind)
  const t = titles[kind] ?? { title: kind, description: "" }

  return (
    <>
      <PageHeader title={t.title} description={t.description} />
      <PageBody>
        {error && <p className="text-sm text-destructive">Could not load the catalog: {error.message}</p>}
        {isLoading && <Skeleton className="h-40" />}
        {data?.length === 0 && (
          <EmptyState title="Nothing registered yet" description="Engineers add assets in agent-assets; they appear here after the platform is deployed." />
        )}
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {data?.map((m) => <AssetCard key={m.id} m={m} />)}
        </div>
      </PageBody>
    </>
  )
}
