import { Link } from "@tanstack/react-router"
import { useOrchestrations } from "@/api/hooks"
import { EmptyState, PageBody, PageHeader } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"

export function AgentsPage() {
  const { data, isLoading, error } = useOrchestrations()
  const rows = (data ?? []).flatMap((o) => o.agents.map((a) => ({ ...a, orchestration: o })))

  return (
    <>
      <PageHeader title="Agents" description="Every agent, across all orchestrations. Agents are edited on their orchestration's canvas." />
      <PageBody>
        {error && <p className="text-sm text-destructive">{error.message}</p>}
        {isLoading && <Skeleton className="h-32" />}
        {data && rows.length === 0 && <EmptyState title="No agents yet" description="Create an orchestration to add agents." />}
        {rows.length > 0 && (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">Agent</th>
                  <th className="px-4 py-2 font-medium">Description</th>
                  <th className="px-4 py-2 font-medium">Orchestration</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={`${r.orchestration.id}-${r.id}`} className="border-t hover:bg-muted/30">
                    <td className="px-4 py-2">
                      <Link
                        to="/orchestrations/$id"
                        params={{ id: r.orchestration.id }}
                        search={{ agent: r.id }}
                        className="font-medium hover:underline"
                      >
                        {r.name}
                      </Link>
                      {r.root && (
                        <Badge variant="secondary" className="ml-2">
                          root
                        </Badge>
                      )}
                    </td>
                    <td className="px-4 py-2 text-muted-foreground">{r.description}</td>
                    <td className="px-4 py-2">{r.orchestration.name}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </PageBody>
    </>
  )
}
