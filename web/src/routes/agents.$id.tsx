import { createFileRoute, Navigate } from "@tanstack/react-router";
import { AgentPage } from "@/pages/agent";

export const Route = createFileRoute("/agents/$id")({ component: AgentRoute });

function AgentRoute() {
  const { id } = Route.useParams();
  // A slug is a package name without its scope, or scope~name (agentSlug).
  if (!/^[a-z0-9][a-z0-9._~-]{0,213}$/.test(id)) return <Navigate to="/agents" replace />;
  return <AgentPage key={id} slug={id} />;
}
