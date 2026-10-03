import { createFileRoute } from "@tanstack/react-router";
import { FrontendPage } from "@/pages/frontend";

export const Route = createFileRoute("/terminal")({
  component: () => <FrontendPage service="ttyd" label="Terminal" path="/terminal/" />,
});
