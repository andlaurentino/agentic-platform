import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "./client"
import type { Kind, Manifest, McpTool, Orchestration, Skill } from "./types"

export const keys = {
  catalog: (kind?: Kind) => ["catalog", kind ?? "all"] as const,
  mcpTools: (id: string) => ["mcp-tools", id] as const,
  skills: ["skills"] as const,
  skill: (id: string) => ["skills", id] as const,
  orchestrations: ["orchestrations"] as const,
  orchestration: (id: string) => ["orchestrations", id] as const,
  draft: (id: string) => ["draft", id] as const,
}

export function useCatalog(kind?: Kind) {
  return useQuery({
    queryKey: keys.catalog(kind),
    queryFn: () => api<Manifest[]>(`/v1/catalog${kind ? `?kind=${kind}` : ""}`),
    staleTime: 5 * 60_000,
  })
}

export function useMcpTools(id: string | undefined) {
  return useQuery({
    queryKey: keys.mcpTools(id ?? ""),
    queryFn: () => api<McpTool[]>(`/v1/mcps/${id}/tools`),
    enabled: !!id,
    staleTime: 60_000,
    retry: false,
  })
}

export function useSkills() {
  return useQuery({ queryKey: keys.skills, queryFn: () => api<Skill[]>("/v1/skills") })
}

export function useSkill(id: string) {
  return useQuery({ queryKey: keys.skill(id), queryFn: () => api<Skill>(`/v1/skills/${id}`) })
}

type SkillInput = Pick<Skill, "name" | "description" | "content">

export function useCreateSkill() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (in_: SkillInput) => api<Skill>("/v1/skills", { method: "POST", body: JSON.stringify(in_) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.skills }),
  })
}

export function useUpdateSkill(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (in_: SkillInput) => api<Skill>(`/v1/skills/${id}`, { method: "PUT", body: JSON.stringify(in_) }),
    onSuccess: (s) => {
      qc.setQueryData(keys.skill(id), s)
      qc.invalidateQueries({ queryKey: keys.skills })
    },
  })
}

export function useDeleteSkill() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`/v1/skills/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.skills }),
  })
}

export function useOrchestrations() {
  return useQuery({ queryKey: keys.orchestrations, queryFn: () => api<Orchestration[]>("/v1/orchestrations") })
}

export function useOrchestration(id: string) {
  return useQuery({ queryKey: keys.orchestration(id), queryFn: () => api<Orchestration>(`/v1/orchestrations/${id}`) })
}

export function useCreateOrchestration() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (in_: { name: string; description: string }) =>
      api<Orchestration>("/v1/orchestrations", { method: "POST", body: JSON.stringify(in_) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.orchestrations }),
  })
}

export function useDeleteOrchestration() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`/v1/orchestrations/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.orchestrations }),
  })
}
