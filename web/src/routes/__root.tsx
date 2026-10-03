import { createRootRoute, Navigate } from "@tanstack/react-router";
import { Shell } from "@/app/shell";

export const Route = createRootRoute({
  component: Shell,
  notFoundComponent: () => <Navigate to="/" replace />,
});
