import {
  Background,
  Controls,
  MarkerType,
  Panel,
  ReactFlow,
  applyNodeChanges,
  useReactFlow,
  type Connection,
  type Edge as FlowEdge,
  type NodeChange,
} from "@xyflow/react"
import { ChevronDown, Plus } from "lucide-react"
import { useCallback, useEffect, useMemo, useState, type MouseEvent } from "react"
import type { EdgeMode } from "@/api/types"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { AgentNode, type AgentNodeType } from "./agent-node"
import { AddAgentItems, CanvasMenu, type MenuState, type MenuTarget } from "./canvas-menu"
import { removeAgent, removeEdge } from "./spec-utils"
import { useEditor } from "./store"

const nodeTypes = { agent: AgentNode }

const edgeLabel: Record<EdgeMode, string> = { transfer: "hands off", tool: "calls as tool" }

function defaultPosition(i: number) {
  return { x: (i % 3) * 300, y: Math.floor(i / 3) * 200 }
}

export function Canvas() {
  const spec = useEditor((s) => s.spec)!
  const selection = useEditor((s) => s.selection)
  const activeAgentId = useEditor((s) => s.activeAgentId)
  const problems = useEditor((s) => s.problems)
  const { edit, select } = useEditor.getState()
  const { screenToFlowPosition } = useReactFlow()
  const [menu, setMenu] = useState<MenuState>({ open: false, x: 0, y: 0, target: { kind: "pane", at: { x: 0, y: 0 } } })

  const openMenu = (e: MouseEvent | globalThis.MouseEvent, target: MenuTarget) => {
    e.preventDefault()
    setMenu({ open: true, x: e.clientX, y: e.clientY, target })
  }

  const errorsByAgent = useMemo(() => {
    const m = new Map<string, number>()
    for (const p of problems) if (p.agentId && p.severity === "error") m.set(p.agentId, (m.get(p.agentId) ?? 0) + 1)
    return m
  }, [problems])

  const buildNodes = useCallback(
    (): AgentNodeType[] =>
      spec.agents.map((a, i) => ({
        id: a.id,
        type: "agent",
        position: spec.layout[a.id] ?? defaultPosition(i),
        selected: selection.kind === "agent" && selection.id === a.id,
        deletable: a.id !== spec.rootAgent,
        data: {
          agent: a,
          isRoot: a.id === spec.rootAgent,
          isActive: a.id === activeAgentId,
          errorCount: errorsByAgent.get(a.id) ?? 0,
          modelLabel: String(a.model.config?.model ?? a.model.asset.split(".").pop() ?? "no model"),
        },
      })),
    [spec, selection, activeAgentId, errorsByAgent],
  )

  // Nodes are local state so dragging is smooth; positions are written to the spec on drag stop.
  const [nodes, setNodes] = useState<AgentNodeType[]>(buildNodes)
  // Re-sync local nodes when the spec, selection or highlights change (React Flow's controlled pattern).
  // oxlint-disable-next-line react/set-state-in-effect
  useEffect(() => setNodes(buildNodes()), [buildNodes])

  const edges: FlowEdge[] = useMemo(
    () =>
      spec.edges.map((e) => ({
        id: `${e.from}->${e.to}`,
        source: e.from,
        target: e.to,
        label: edgeLabel[e.mode],
        animated: e.from === activeAgentId || e.to === activeAgentId,
        selected: selection.kind === "edge" && selection.from === e.from && selection.to === e.to,
        markerEnd: { type: MarkerType.ArrowClosed },
        style: e.mode === "tool" ? { strokeDasharray: "6 4" } : undefined,
        data: { mode: e.mode },
      })),
    [spec.edges, activeAgentId, selection],
  )

  const onNodesChange = useCallback(
    (changes: NodeChange<AgentNodeType>[]) => {
      setNodes((ns) => applyNodeChanges(changes, ns))
      for (const c of changes) {
        if (c.type === "select" && c.selected) select({ kind: "agent", id: c.id })
      }
    },
    [select],
  )

  const isValidConnection = useCallback(
    (c: Connection | FlowEdge) =>
      c.source !== c.target && c.target !== spec.rootAgent && !spec.edges.some((e) => e.from === c.source && e.to === c.target),
    [spec],
  )

  const onConnect = useCallback(
    (c: Connection) =>
      edit((s) => {
        s.edges.push({ from: c.source, to: c.target, mode: "transfer" })
      }),
    [edit],
  )

  // The toolbar adds agents below the lowest one.
  const belowLast = () => ({ x: 0, y: Math.max(-200, ...Object.values(spec.layout).map((p) => p.y)) + 200 })

  return (
    <>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onNodeDragStop={(_, node) =>
          edit((s) => {
            s.layout[node.id] = { x: Math.round(node.position.x), y: Math.round(node.position.y) }
          })
        }
        onConnect={onConnect}
        isValidConnection={isValidConnection}
        onEdgeClick={(_, edge) => select({ kind: "edge", from: edge.source, to: edge.target })}
        // Deleting takes agents off this canvas; they stay in the agent library.
        onNodesDelete={(deleted) =>
          edit((s) => {
            for (const n of deleted) removeAgent(s, n.id)
          })
        }
        onEdgesDelete={(deleted) =>
          edit((s) => {
            for (const e of deleted) removeEdge(s, e.source, e.target)
          })
        }
        onPaneClick={() => select({ kind: "orchestration" })}
        onPaneContextMenu={(e) => openMenu(e, { kind: "pane", at: screenToFlowPosition({ x: e.clientX, y: e.clientY }) })}
        onNodeContextMenu={(e, node) => {
          select({ kind: "agent", id: node.id })
          openMenu(e, { kind: "agent", id: node.id })
        }}
        onEdgeContextMenu={(e, edge) => {
          select({ kind: "edge", from: edge.source, to: edge.target })
          openMenu(e, { kind: "edge", from: edge.source, to: edge.target })
        }}
        fitView
        fitViewOptions={{ maxZoom: 1 }}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={20} />
        <Controls showInteractive={false} />
        <Panel position="top-left" className="flex items-center gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="sm">
                <Plus /> Add agent <ChevronDown />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="w-56">
              <AddAgentItems at={belowLast} />
            </DropdownMenuContent>
          </DropdownMenu>
          <p className="hidden rounded-md bg-background/80 px-2 py-1 text-xs text-muted-foreground backdrop-blur md:block">
            Right-click the canvas, an agent or an arrow for options. Drag from an agent's bottom handle to another agent to delegate.
          </p>
        </Panel>
      </ReactFlow>
      <CanvasMenu menu={menu} onOpenChange={(open) => setMenu((m) => ({ ...m, open }))} />
    </>
  )
}
