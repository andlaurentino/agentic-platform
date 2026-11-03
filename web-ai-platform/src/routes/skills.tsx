import { Link, useNavigate } from "@tanstack/react-router"
import { Plus } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { useCreateSkill, useSkills } from "@/api/hooks"
import { EmptyState, PageBody, PageHeader } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"

function NewSkillDialog() {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const create = useCreateSkill()
  const navigate = useNavigate()

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      const s = await create.mutateAsync({ name, description, content: "" })
      setOpen(false)
      navigate({ to: "/skills/$id", params: { id: s.id } })
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus /> New skill
        </Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-4">
          <DialogHeader>
            <DialogTitle>New skill</DialogTitle>
          </DialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="skill-name">Name</Label>
            <Input id="skill-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Refund policy" autoFocus />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="skill-desc">Description</Label>
            <Input id="skill-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What this skill teaches an agent" />
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

export function SkillsPage() {
  const { data, isLoading, error } = useSkills()
  return (
    <>
      <PageHeader title="Skills" description="Reusable instructions you can attach to any agent." actions={<NewSkillDialog />} />
      <PageBody>
        {error && <p className="text-sm text-destructive">{error.message}</p>}
        {isLoading && <Skeleton className="h-32" />}
        {data?.length === 0 && (
          <EmptyState title="No skills yet" description="A skill is a block of instructions, like a policy or a tone of voice, that you can reuse across agents." />
        )}
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {data?.map((s) => (
            <Link key={s.id} to="/skills/$id" params={{ id: s.id }}>
              <Card className="h-full transition-colors hover:bg-muted/50">
                <CardHeader>
                  <CardTitle className="text-base">{s.name}</CardTitle>
                  <CardDescription>{s.description || "No description"}</CardDescription>
                  <p className="text-xs text-muted-foreground">
                    v{s.version} · updated {new Date(s.updatedAt).toLocaleString()}
                  </p>
                </CardHeader>
              </Card>
            </Link>
          ))}
        </div>
      </PageBody>
    </>
  )
}
