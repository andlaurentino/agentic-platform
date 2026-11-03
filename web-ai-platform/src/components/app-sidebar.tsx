import { Link, useRouterState } from "@tanstack/react-router"
import { Bot, BrainCircuit, Network, Plug, ShieldCheck, Sparkles, Workflow, Wrench } from "lucide-react"
import { useEffect, useState } from "react"
import { Input } from "@/components/ui/input"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar"
import { getEditorName, setEditorName } from "@/lib/editor-name"

const build = [
  { to: "/orchestrations", label: "Orchestrations", icon: Workflow },
  { to: "/agents", label: "Agents", icon: Bot },
  { to: "/skills", label: "Skills", icon: Sparkles },
] as const

const assets = [
  { kind: "tool", label: "Tools", icon: Wrench },
  { kind: "mcp", label: "MCPs", icon: Plug },
  { kind: "guardrail", label: "Guardrails", icon: ShieldCheck },
  { kind: "llm", label: "LLMs", icon: BrainCircuit },
] as const

export function AppSidebar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const { state } = useSidebar()
  const [name, setName] = useState(getEditorName)
  useEffect(() => setEditorName(name), [name])

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild>
              <Link to="/orchestrations">
                <div className="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground">
                  <Network className="size-4" />
                </div>
                <div className="grid leading-tight">
                  <span className="font-semibold">AI Platform</span>
                  <span className="text-xs text-muted-foreground">Agent builder</span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Build</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {build.map((item) => (
                <SidebarMenuItem key={item.to}>
                  <SidebarMenuButton asChild tooltip={item.label} isActive={pathname.startsWith(item.to)}>
                    <Link to={item.to}>
                      <item.icon />
                      <span>{item.label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>Assets</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {assets.map((item) => (
                <SidebarMenuItem key={item.kind}>
                  <SidebarMenuButton asChild tooltip={item.label} isActive={pathname === `/catalog/${item.kind}`}>
                    <Link to="/catalog/$kind" params={{ kind: item.kind }}>
                      <item.icon />
                      <span>{item.label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      {state === "expanded" && (
        <SidebarFooter>
          <label className="px-2 text-xs text-muted-foreground" htmlFor="editor-name">
            Your name (shown on saves)
          </label>
          <Input id="editor-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Ana" />
        </SidebarFooter>
      )}
      <SidebarRail />
    </Sidebar>
  )
}
