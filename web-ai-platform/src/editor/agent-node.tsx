import { Handle, Position, type Node, type NodeProps } from "@xyflow/react"
import { Crown, Plug, ShieldCheck, Sparkles, Wrench } from "lucide-react"
import { memo } from "react"
import type { AgentSpec } from "@/api/types"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

export type AgentNodeData = {
  agent: AgentSpec
  isRoot: boolean
  isActive: boolean
  errorCount: number
  modelLabel: string
}

export type AgentNodeType = Node<AgentNodeData, "agent">

function AgentNodeImpl({ data, selected }: NodeProps<AgentNodeType>) {
  const { agent, isRoot, isActive, errorCount, modelLabel } = data
  const mcpTools = agent.tools.flatMap((t) => t.tools ?? [])
  const nativeTools = agent.tools.filter((t) => t.asset).length
  return (
    <div
      className={cn(
        "w-60 rounded-lg border bg-card p-3 text-card-foreground shadow-sm transition-shadow",
        selected && "ring-2 ring-primary",
        isActive && "shadow-lg ring-2 ring-emerald-500 ring-offset-2",
        errorCount > 0 && !selected && "border-destructive",
      )}
    >
      {!isRoot && <Handle type="target" position={Position.Top} className="!size-2.5" />}
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            {isRoot && <Crown className="size-3.5 shrink-0 text-amber-500" aria-label="Root agent" />}
            <p className="truncate text-sm font-semibold">{agent.name || "Untitled agent"}</p>
          </div>
          <p className="line-clamp-2 text-xs text-muted-foreground">{agent.description || "No description"}</p>
        </div>
        {isActive && <span className="mt-1 size-2 shrink-0 animate-pulse rounded-full bg-emerald-500" />}
      </div>
      <div className="mt-2 flex flex-wrap gap-1">
        <Badge variant="secondary" className="text-[10px]">
          {modelLabel}
        </Badge>
        {agent.skills.length > 0 && (
          <Badge variant="outline" className="gap-1 text-[10px]">
            <Sparkles className="size-3" /> {agent.skills.length}
          </Badge>
        )}
        {nativeTools > 0 && (
          <Badge variant="outline" className="gap-1 text-[10px]">
            <Wrench className="size-3" /> {nativeTools}
          </Badge>
        )}
        {mcpTools.length > 0 && (
          <Badge variant="outline" className="gap-1 text-[10px]" title={mcpTools.join(", ")}>
            <Plug className="size-3" /> {mcpTools.length}
          </Badge>
        )}
        {agent.guardrails.length > 0 && (
          <Badge variant="outline" className="gap-1 text-[10px]">
            <ShieldCheck className="size-3" /> {agent.guardrails.length}
          </Badge>
        )}
        {errorCount > 0 && (
          <Badge variant="destructive" className="text-[10px]">
            {errorCount} {errorCount === 1 ? "problem" : "problems"}
          </Badge>
        )}
      </div>
      <Handle type="source" position={Position.Bottom} className="!size-2.5" />
    </div>
  )
}

export const AgentNode = memo(AgentNodeImpl)
