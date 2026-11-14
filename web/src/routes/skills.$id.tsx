import { createFileRoute, Navigate } from "@tanstack/react-router";
import { SourcePage } from "@/pages/source";

export const Route = createFileRoute("/skills/$id")({
  validateSearch: (search: Record<string, unknown>): { q?: string } =>
    typeof search.q === "string" && search.q ? { q: search.q.slice(0, 64) } : {},
  component: SourceRoute,
});

function SourceRoute() {
  const { id } = Route.useParams();
  const { q } = Route.useSearch();
  if (!/^[a-z0-9][a-z0-9-]{0,63}$/.test(id)) return <Navigate to="/skills" replace />;
  return <SourcePage key={id} id={id} query={q} />;
}
