import { Link, useNavigate } from "@tanstack/react-router"
import { Bot, MoreHorizontal, Plus, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { useCreateOrchestration, useDeleteOrchestration, useOrchestrations } from "@/api/hooks"
import { EmptyState, PageBody, PageHeader } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"

function NewOrchestrationDialog() {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const create = useCreateOrchestration()
  const navigate = useNavigate()

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      const o = await create.mutateAsync({ name, description })
      setOpen(false)
      navigate({ to: "/orchestrations/$id", params: { id: o.id } })
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus /> New orchestration
        </Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-4">
          <DialogHeader>
            <DialogTitle>New orchestration</DialogTitle>
          </DialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="orch-name">Name</Label>
            <Input id="orch-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Customer care" autoFocus />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="orch-desc">Description</Label>
            <Input id="orch-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What this set of agents is for" />
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

export function OrchestrationsPage() {
  const { data, isLoading, error } = useOrchestrations()
  const del = useDeleteOrchestration()

  return (
    <>
      <PageHeader
        title="Orchestrations"
        description="A conversation orchestration is a set of agents that work together, starting from a root agent."
        actions={<NewOrchestrationDialog />}
      />
      <PageBody>
        {error && <p className="text-sm text-destructive">Could not reach the platform API: {error.message}</p>}
        {isLoading && <Skeleton className="h-32" />}
        {data?.length === 0 && (
          <EmptyState title="No orchestrations yet" description="Create one to start building agents on the canvas." action={<NewOrchestrationDialog />} />
        )}
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {data?.map((o) => (
            <Card key={o.id} className="relative transition-colors hover:bg-muted/50">
              <Link to="/orchestrations/$id" params={{ id: o.id }} className="absolute inset-0" aria-label={`Open ${o.name}`} />
              <CardHeader>
                <div className="flex items-start justify-between gap-2">
                  <CardTitle className="text-base">{o.name}</CardTitle>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon" className="relative z-10 -mt-1 size-7" aria-label="More">
                        <MoreHorizontal />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={async () => {
                          await del.mutateAsync(o.id)
                          toast.success(`Deleted ${o.name}`)
                        }}
                      >
                        <Trash2 /> Delete
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
                <CardDescription>{o.description || "No description"}</CardDescription>
                <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Bot className="size-3.5" /> {o.agents.length} {o.agents.length === 1 ? "agent" : "agents"} · edited{" "}
                  {new Date(o.updatedAt).toLocaleString()}
                </p>
              </CardHeader>
            </Card>
          ))}
        </div>
      </PageBody>
    </>
  )
}
