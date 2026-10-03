import { Page, PageHeader } from "@/components/page";
import { useTitle } from "@/lib/use-title";

export function AgentsPage() {
  useTitle("Agents");
  return (
    <Page>
      <PageHeader
        title="Agents"
        description="Pinned agent CLIs and MCP servers. Switch each on or off and give them API keys."
      />
    </Page>
  );
}
