import { createFileRoute } from "@tanstack/react-router";
import { FrontendPage } from "@/pages/frontend";

export const Route = createFileRoute("/ide")({
  component: () => <FrontendPage service="code_server" label="IDE" path="/ide/" />,
});
