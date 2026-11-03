// Types mirror the ms-ai-platform JSON API (hand-written for this slice).

export type Kind = "llm" | "tool" | "mcp" | "guardrail"

export interface JSONSchema {
  type?: string | string[]
  title?: string
  description?: string
  enum?: unknown[]
  /** Labeled choices: [{ const, title }]. Used for model pickers. */
  oneOf?: { const: unknown; title?: string }[]
  default?: unknown
  properties?: Record<string, JSONSchema>
  items?: JSONSchema
  minimum?: number
  maximum?: number
  format?: string
  uniqueItems?: boolean
}

export interface Manifest {
  id: string
  kind: Kind
  domain: string
  name: string
  description: string
  owner: string
  tags?: string[]
  lifecycle: "experimental" | "stable" | "deprecated"
  configSchema: JSONSchema
  secrets?: { name: string; description?: string }[]
  extra?: {
    // LLM providers (from llms.yaml)
    providerType?: "anthropic" | "openai"
    baseURL?: string
    apiKeyEnv?: string
    credentialsConfigured?: boolean
    models?: { id: string; label?: string; default?: boolean }[]
    discovery?: { status: "off" | "ok" | "failed"; error?: string; count: number }
    // guardrails
    level?: "L1" | "L2"
    stages?: string[]
    allowedTools?: string[]
    transport?: string
    auth?: string
    sideEffects?: string
  }
}

export interface McpTool {
  name: string
  description: string
  inputSchema?: JSONSchema
}

export interface Skill {
  id: string
  name: string
  description: string
  content: string
  version: number
  createdAt: string
  updatedAt: string
}

export interface AgentSummary {
  id: string
  name: string
  description: string
  root: boolean
}

export interface Orchestration {
  id: string
  name: string
  description: string
  createdAt: string
  updatedAt: string
  agents: AgentSummary[]
}

export type Config = Record<string, unknown>

export interface AssetRef {
  asset: string
  config?: Config
}

export interface ToolRef {
  asset?: string
  config?: Config
  mcp?: string
  tools?: string[]
}

export type EdgeMode = "transfer" | "tool"

export interface Edge {
  from: string
  to: string
  mode: EdgeMode
}

export interface AgentSpec {
  id: string
  name: string
  description: string
  instructions: string
  model: AssetRef
  skills: string[]
  tools: ToolRef[]
  guardrails: AssetRef[]
}

export interface OrchestrationSpec {
  rootAgent: string
  agents: AgentSpec[]
  edges: Edge[]
  guardrails: AssetRef[]
  layout: Record<string, { x: number; y: number }>
}

export interface Draft {
  orchestrationId: string
  spec: OrchestrationSpec
  version: number
  updatedAt: string
  updatedByName: string
}

export interface Problem {
  path: string
  agentId?: string
  message: string
  severity: "error" | "warning"
}

export interface GuardrailDecision {
  guardrailId: string
  name: string
  level: "L1" | "L2"
  stage: string
  action: "allow" | "redact" | "block" | "escalate"
  reason?: string
  labels?: string[]
  agentId?: string
}
