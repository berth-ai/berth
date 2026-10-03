import { createFileRoute, Navigate } from "@tanstack/react-router";
import { JobPage } from "@/pages/activity";

export const Route = createFileRoute("/activity/$id")({ component: JobRoute });

function JobRoute() {
  const { id } = Route.useParams();
  if (!/^\d+$/.test(id)) return <Navigate to="/activity" replace />;
  return <JobPage id={Number(id)} />;
}
