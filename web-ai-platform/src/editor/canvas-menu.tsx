import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { Crown, ExternalLink, Library, Pencil, Plus, Trash2, Unlink } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"
import { api } from "@/api/client"
import { createAgent, keys, useAgents } from "@/api/hooks"
import type { AgentRecord, EdgeMode } from "@/api/types"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuPortal,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { getEditorName } from "@/lib/editor-name"
import { makeRoot, newAgent, removeAgent, removeEdge } from "./spec-utils"
import { useEditor } from "./store"

type Position = { x: number; y: number }

export type MenuTarget = { kind: "pane"; at: Position } | { kind: "agent"; id: string } | { kind: "edge"; from: string; to: string }

/** A context menu: where it opens on screen and what was right-clicked. */
export type MenuState = { open: boolean; x: number; y: number; target: MenuTarget }

/** Adds a new or an existing library agent to the canvas at a flow position. */
function useAddAgent() {
  const qc = useQueryClient()
  return {
    addNew: async (at: Position) => {
      try {
        const record = await createAgent(newAgent(useEditor.getState().spec), getEditorName())
        void qc.invalidateQueries({ queryKey: keys.agents })
        useEditor.getState().addAgent(record, at)
      } catch (err) {
        toast.error(`Could not create the agent: ${(err as Error).message}`)
      }
    },
    addExisting: async (id: string, at: Position) => {
      try {
        // Fetch the latest version, so a later save does not conflict with an outdated list.
        useEditor.getState().addAgent(await api<AgentRecord>(`/v1/agents/${id}`), at)
      } catch (err) {
        toast.error(`Could not add the agent: ${(err as Error).message}`)
      }
    },
  }
}

/** Library agents that are not on the canvas yet, with a filter. */
function ExistingAgents({ onPick }: { onPick: (id: string) => void }) {
  const { data, isLoading, error } = useAgents()
  const onCanvas = useEditor((s) => s.spec?.agents)
  const [filter, setFilter] = useState("")
  const available = useMemo(() => {
    const ids = new Set(onCanvas?.map((a) => a.id))
    const q = filter.trim().toLowerCase()
    return (data ?? []).filter((a) => !ids.has(a.id) && (!q || `${a.name} ${a.description}`.toLowerCase().includes(q)))
  }, [data, onCanvas, filter])

  return (
    <>
      <div className="p-1">
        <Input
          autoFocus
          className="h-8 text-sm"
          placeholder="Filter agents…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          // Keep the menu's type-ahead from taking the keys.
          onKeyDown={(e) => e.key !== "Escape" && e.stopPropagation()}
        />
      </div>
      <div className="max-h-72 overflow-y-auto">
        {isLoading && <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">Loading…</DropdownMenuLabel>}
        {error && <DropdownMenuLabel className="text-xs font-normal text-destructive">{error.message}</DropdownMenuLabel>}
        {data && available.length === 0 && (
          <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
            {filter ? "No matching agents." : "Every agent in the library is already here."}
          </DropdownMenuLabel>
        )}
        {available.map((a) => (
          <DropdownMenuItem key={a.id} className="grid gap-0 py-1.5" onSelect={() => onPick(a.id)}>
            <span className="truncate font-medium">{a.name}</span>
            <span className="truncate text-xs text-muted-foreground">
              {a.usedBy.length === 0
                ? "Not used yet"
                : `Used in ${a.usedBy.length} ${a.usedBy.length === 1 ? "orchestration" : "orchestrations"}`}
              {a.description && ` · ${a.description}`}
            </span>
          </DropdownMenuItem>
        ))}
      </div>
    </>
  )
}

/** "New agent" and "Add existing agent" items, shared by the canvas menu and the toolbar button. */
export function AddAgentItems({ at }: { at: () => Position }) {
  const { addNew, addExisting } = useAddAgent()
  return (
    <>
      <DropdownMenuItem onSelect={() => void addNew(at())}>
        <Plus /> New agent
      </DropdownMenuItem>
      <DropdownMenuSub>
        <DropdownMenuSubTrigger>
          <Library /> Add existing agent
        </DropdownMenuSubTrigger>
        <DropdownMenuPortal>
          <DropdownMenuSubContent className="w-72">
            <ExistingAgents onPick={(id) => void addExisting(id, at())} />
          </DropdownMenuSubContent>
        </DropdownMenuPortal>
      </DropdownMenuSub>
    </>
  )
}

function AgentItems({ id }: { id: string }) {
  const navigate = useNavigate()
  const isRoot = useEditor((s) => s.spec?.rootAgent === id)
  const { edit, select } = useEditor.getState()
  return (
    <>
      <DropdownMenuItem onSelect={() => select({ kind: "agent", id })}>
        <Pencil /> Edit
      </DropdownMenuItem>
      <DropdownMenuItem onSelect={() => void navigate({ to: "/agents/$id", params: { id } })}>
        <ExternalLink /> Open agent page
      </DropdownMenuItem>
      {!isRoot && (
        <>
          <DropdownMenuItem onSelect={() => edit((s) => makeRoot(s, id))}>
            <Crown /> Make root
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onSelect={() => {
              edit((s) => removeAgent(s, id))
              select({ kind: "orchestration" })
            }}
          >
            <Trash2 /> Remove from orchestration
          </DropdownMenuItem>
        </>
      )}
    </>
  )
}

function EdgeItems({ from, to }: { from: string; to: string }) {
  const mode = useEditor((s) => s.spec?.edges.find((e) => e.from === from && e.to === to)?.mode)
  const { edit, select } = useEditor.getState()
  if (!mode) return null
  return (
    <>
      <DropdownMenuRadioGroup
        value={mode}
        onValueChange={(m) =>
          edit((s) => {
            const e = s.edges.find((x) => x.from === from && x.to === to)
            if (e) e.mode = m as EdgeMode
          })
        }
      >
        <DropdownMenuRadioItem value="transfer">Hands off</DropdownMenuRadioItem>
        <DropdownMenuRadioItem value="tool">Calls as tool</DropdownMenuRadioItem>
      </DropdownMenuRadioGroup>
      <DropdownMenuSeparator />
      <DropdownMenuItem onSelect={() => select({ kind: "edge", from, to })}>
        <Pencil /> Edit connection
      </DropdownMenuItem>
      <DropdownMenuItem
        variant="destructive"
        onSelect={() => {
          edit((s) => removeEdge(s, from, to))
          select({ kind: "orchestration" })
        }}
      >
        <Unlink /> Remove connection
      </DropdownMenuItem>
    </>
  )
}

/** The canvas's right-click menu, anchored at the cursor. */
export function CanvasMenu({ menu, onOpenChange }: { menu: MenuState; onOpenChange: (open: boolean) => void }) {
  const { target } = menu
  return (
    <DropdownMenu open={menu.open} onOpenChange={onOpenChange} modal={false}>
      <DropdownMenuTrigger asChild>
        <span aria-hidden className="pointer-events-none fixed size-0" style={{ left: menu.x, top: menu.y }} />
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-56" sideOffset={0} onCloseAutoFocus={(e) => e.preventDefault()}>
        {target.kind === "pane" && <AddAgentItems at={() => target.at} />}
        {target.kind === "agent" && <AgentItems id={target.id} />}
        {target.kind === "edge" && <EdgeItems from={target.from} to={target.to} />}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
