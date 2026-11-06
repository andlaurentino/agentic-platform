import { Link, useNavigate } from "@tanstack/react-router"
import { Plus } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { useAgents, useCreateAgent } from "@/api/hooks"
import { EmptyState, PageBody, PageHeader } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { newAgent } from "@/editor/spec-utils"
import { getEditorName } from "@/lib/editor-name"

function NewAgentDialog() {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const create = useCreateAgent()
  const navigate = useNavigate()

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      const a = await create.mutateAsync({ agent: { ...newAgent(null), name, description }, by: getEditorName() })
      setOpen(false)
      navigate({ to: "/agents/$id", params: { id: a.id } })
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus /> New agent
        </Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-4">
          <DialogHeader>
            <DialogTitle>New agent</DialogTitle>
          </DialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="agent-name">Name</Label>
            <Input id="agent-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Refunds specialist" autoFocus />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="agent-desc">Description</Label>
            <Input
              id="agent-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="When should other agents hand off to this one?"
            />
          </div>
          <DialogFooter>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function AgentsPage() {
  const { data, isLoading, error } = useAgents()

  return (
    <>
      <PageHeader
        title="Agents"
        description="The agent library. An agent can be used in any number of orchestrations; editing it changes it everywhere."
        actions={<NewAgentDialog />}
      />
      <PageBody>
        {error && <p className="text-sm text-destructive">{error.message}</p>}
        {isLoading && <Skeleton className="h-32" />}
        {data?.length === 0 && <EmptyState title="No agents yet" description="Create an agent, then add it to an orchestration's canvas." action={<NewAgentDialog />} />}
        {data && data.length > 0 && (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">Agent</th>
                  <th className="px-4 py-2 font-medium">Description</th>
                  <th className="px-4 py-2 font-medium">Used in</th>
                </tr>
              </thead>
              <tbody>
                {data.map((a) => (
                  <tr key={a.id} className="border-t hover:bg-muted/30">
                    <td className="px-4 py-2">
                      <Link to="/agents/$id" params={{ id: a.id }} className="font-medium hover:underline">
                        {a.name}
                      </Link>
                    </td>
                    <td className="px-4 py-2 text-muted-foreground">{a.description}</td>
                    <td className="px-4 py-2">
                      {a.usedBy.length === 0 && <span className="text-muted-foreground">Not used</span>}
                      {a.usedBy.map((o, i) => (
                        <span key={o.id}>
                          {i > 0 && ", "}
                          <Link to="/orchestrations/$id" params={{ id: o.id }} search={{ agent: a.id }} className="hover:underline">
                            {o.name}
                          </Link>
                        </span>
                      ))}
                    </td>
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
