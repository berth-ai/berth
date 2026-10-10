import { createFileRoute, Navigate } from "@tanstack/react-router";
import { WorkspacePage } from "@/pages/workspace";
import { parseWorkspaceTab, type WorkspaceTab } from "@/pages/workspaces-logic";

export const Route = createFileRoute("/workspaces/$id")({
  validateSearch: (search: Record<string, unknown>): { tab?: WorkspaceTab } => {
    const tab = parseWorkspaceTab(search.tab);
    return tab === "overview" ? {} : { tab };
  },
  component: WorkspaceRoute,
});

function WorkspaceRoute() {
  const { id } = Route.useParams();
  // Parsed again, as in the Tools route: an unknown ?tab= survives {}.
  const tab = parseWorkspaceTab(Route.useSearch().tab);
  const navigate = Route.useNavigate();
  if (!/^[a-z0-9][a-z0-9-]{0,63}$/.test(id)) return <Navigate to="/workspaces" replace />;
  return (
    <WorkspacePage
      key={id}
      id={id}
      tab={tab}
      onTab={(t) => void navigate({ search: t === "overview" ? {} : { tab: t }, replace: true })}
    />
  );
}
