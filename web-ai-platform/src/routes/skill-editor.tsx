import { useNavigate, useParams } from "@tanstack/react-router"
import { Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { useDeleteSkill, useSkill, useUpdateSkill } from "@/api/hooks"
import type { Skill } from "@/api/types"
import { PageBody, PageHeader } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"

export function SkillEditorPage() {
  const { id } = useParams({ strict: false }) as { id: string }
  const { data, isLoading, error } = useSkill(id)
  if (error) return <PageBody>{error.message}</PageBody>
  if (isLoading || !data)
    return (
      <>
        <PageHeader title="Skill" />
        <PageBody>
          <Skeleton className="h-64" />
        </PageBody>
      </>
    )
  // Remount the form whenever a new version is loaded.
  return <SkillForm key={`${data.id}-${data.version}`} skill={data} />
}

function SkillForm({ skill: data }: { skill: Skill }) {
  const id = data.id
  const update = useUpdateSkill(id)
  const del = useDeleteSkill()
  const navigate = useNavigate()
  const [form, setForm] = useState({ name: data.name, description: data.description, content: data.content })

  const dirty = form.name !== data.name || form.description !== data.description || form.content !== data.content

  const save = async () => {
    try {
      await update.mutateAsync(form)
      toast.success("Skill saved. Agents using it pick it up on their next run.")
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const remove = async () => {
    await del.mutateAsync(id)
    toast.success("Skill deleted")
    navigate({ to: "/skills" })
  }

  return (
    <>
      <PageHeader
        title={data.name}
        description={`Version ${data.version}`}
        actions={
          <>
            <Button variant="ghost" size="sm" onClick={remove} disabled={del.isPending}>
              <Trash2 /> Delete
            </Button>
            <Button size="sm" onClick={save} disabled={!dirty || !form.name.trim() || update.isPending}>
              {update.isPending ? "Saving…" : "Save"}
            </Button>
          </>
        }
      />
      <PageBody>
        <div className="mx-auto grid max-w-3xl gap-4">
            <div className="grid gap-1.5">
              <Label htmlFor="name">Name</Label>
              <Input id="name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="description">Description</Label>
              <Input id="description" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="content">Instructions (Markdown)</Label>
              <Textarea
                id="content"
                className="min-h-80 font-mono text-sm"
                value={form.content}
                onChange={(e) => setForm({ ...form, content: e.target.value })}
                placeholder={"- Always greet the customer by name.\n- Never promise refunds over 100 EUR without escalation."}
              />
              <p className="text-xs text-muted-foreground">Added to the instructions of every agent that uses this skill.</p>
            </div>
        </div>
      </PageBody>
    </>
  )
}
