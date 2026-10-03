import { createFileRoute } from "@tanstack/react-router";
import { ToolsPage } from "@/pages/tools";
import { parseToolsTab, type ToolsTab } from "@/pages/tools-logic";

export const Route = createFileRoute("/tools")({
  validateSearch: (search: Record<string, unknown>): { tab?: ToolsTab } => {
    const tab = parseToolsTab(search.tab);
    return tab === "builtin" ? {} : { tab };
  },
  component: ToolsRoute,
});

function ToolsRoute() {
  // Parsed again: the router merges this route's search over the root's raw
  // one, so an unknown ?tab= survives validateSearch returning {}.
  const tab = parseToolsTab(Route.useSearch().tab);
  const navigate = Route.useNavigate();
  return (
    <ToolsPage
      tab={tab}
      onTab={(t) => void navigate({ search: t === "builtin" ? {} : { tab: t }, replace: true })}
    />
  );
}
