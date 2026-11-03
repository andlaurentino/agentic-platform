import {
  Background,
  Controls,
  MarkerType,
  Panel,
  ReactFlow,
  applyNodeChanges,
  type Connection,
  type Edge as FlowEdge,
  type NodeChange,
} from "@xyflow/react"
import { Plus } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import type { EdgeMode } from "@/api/types"
import { Button } from "@/components/ui/button"
import { AgentNode, type AgentNodeType } from "./agent-node"
import { newAgent } from "./spec-utils"
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
        markerEnd: { type: MarkerType.ArrowClosed },
        style: e.mode === "tool" ? { strokeDasharray: "6 4" } : undefined,
        data: { mode: e.mode },
      })),
    [spec.edges, activeAgentId],
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

  const addAgent = () => {
    const a = newAgent(spec)
    const maxY = Math.max(0, ...Object.values(spec.layout).map((p) => p.y))
    edit((s) => {
      s.agents.push(a)
      s.layout[a.id] = { x: 0, y: maxY + 200 }
    })
    select({ kind: "agent", id: a.id })
  }

  return (
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
      onEdgeClick={(_, edge) =>
        edit((s) => {
          const e = s.edges.find((x) => `${x.from}->${x.to}` === edge.id)
          if (e) e.mode = e.mode === "transfer" ? "tool" : "transfer"
        })
      }
      onNodesDelete={(deleted) =>
        edit((s) => {
          const ids = new Set(deleted.map((n) => n.id).filter((id) => id !== s.rootAgent))
          s.agents = s.agents.filter((a) => !ids.has(a.id))
          s.edges = s.edges.filter((e) => !ids.has(e.from) && !ids.has(e.to))
          for (const id of ids) delete s.layout[id]
        })
      }
      onEdgesDelete={(deleted) =>
        edit((s) => {
          const ids = new Set(deleted.map((e) => e.id))
          s.edges = s.edges.filter((e) => !ids.has(`${e.from}->${e.to}`))
        })
      }
      onPaneClick={() => select({ kind: "orchestration" })}
      fitView
      fitViewOptions={{ maxZoom: 1 }}
      proOptions={{ hideAttribution: true }}
    >
      <Background gap={20} />
      <Controls showInteractive={false} />
      <Panel position="top-left" className="flex items-center gap-2">
        <Button size="sm" onClick={addAgent}>
          <Plus /> Add agent
        </Button>
        <p className="hidden rounded-md bg-background/80 px-2 py-1 text-xs text-muted-foreground backdrop-blur md:block">
          Drag from an agent's bottom handle to another agent to delegate. Click an arrow to switch between hand-off and call-as-tool.
        </p>
      </Panel>
    </ReactFlow>
  )
}
