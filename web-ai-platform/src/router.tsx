import { Outlet, createRootRoute, createRoute, createRouter, redirect } from "@tanstack/react-router"
import { AppSidebar } from "@/components/app-sidebar"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { AgentEditorPage } from "@/routes/agent-editor"
import { AgentsPage } from "@/routes/agents"
import { CatalogPage } from "@/routes/catalog"
import { OrchestrationEditorPage } from "@/routes/orchestration-editor"
import { OrchestrationsPage } from "@/routes/orchestrations"
import { SkillEditorPage } from "@/routes/skill-editor"
import { SkillsPage } from "@/routes/skills"

const rootRoute = createRootRoute({
  component: () => (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset className="h-svh overflow-hidden">
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  ),
})

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/orchestrations" })
  },
})

const orchestrationsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/orchestrations", component: OrchestrationsPage })

export const orchestrationEditorRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/orchestrations/$id",
  validateSearch: (s: Record<string, unknown>): { agent?: string } => ({
    agent: typeof s.agent === "string" ? s.agent : undefined,
  }),
  component: OrchestrationEditorPage,
})

const agentsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/agents", component: AgentsPage })
const agentRoute = createRoute({ getParentRoute: () => rootRoute, path: "/agents/$id", component: AgentEditorPage })
const skillsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/skills", component: SkillsPage })
const skillRoute = createRoute({ getParentRoute: () => rootRoute, path: "/skills/$id", component: SkillEditorPage })
const catalogRoute = createRoute({ getParentRoute: () => rootRoute, path: "/catalog/$kind", component: CatalogPage })

export const router = createRouter({
  routeTree: rootRoute.addChildren([
    indexRoute,
    orchestrationsRoute,
    orchestrationEditorRoute,
    agentsRoute,
    agentRoute,
    skillsRoute,
    skillRoute,
    catalogRoute,
  ]),
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}
