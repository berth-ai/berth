import { createFileRoute } from "@tanstack/react-router";
import { FrontendPage } from "@/pages/frontend";

export const Route = createFileRoute("/ide")({
  component: () => <FrontendPage service="code_server" frontend="ide" label="IDE" path="/ide/" />,
});
