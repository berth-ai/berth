import { createFileRoute, Navigate } from "@tanstack/react-router";
import { SourcePage } from "@/pages/source";

export const Route = createFileRoute("/skills/$id")({ component: SourceRoute });

function SourceRoute() {
  const { id } = Route.useParams();
  if (!/^[a-z0-9][a-z0-9-]{0,63}$/.test(id)) return <Navigate to="/skills" replace />;
  return <SourcePage key={id} id={id} />;
}
