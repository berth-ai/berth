import { useStatus } from "@/api/queries";
import { Page, PageHeader } from "@/components/page";
import { useTitle } from "@/lib/use-title";
import { PackageSection } from "./package-table";

// MCP servers: the lock's MCP packages, switched on or off and given keys.
export function McpPage() {
  useTitle("MCP servers");
  const status = useStatus();
  return (
    <Page>
      <PageHeader
        title="MCP servers"
        description="Tools your agents can call. Berth registers each enabled server with every enabled agent."
      />
      <PackageSection
        title="Servers"
        kind="mcp"
        nameHeader="Server"
        noun={["server", "servers"]}
        all={status.data ? (status.data.packages ?? []) : undefined}
        empty="No MCP servers installed yet."
      />
    </Page>
  );
}
