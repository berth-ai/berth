import { Page, PageHeader } from "@/components/page";
import { useTitle } from "@/lib/use-title";

export function ToolsPage() {
  useTitle("Tools");
  return (
    <Page>
      <PageHeader
        title="Tools"
        description="The pinned toolchain, your own tools, and curated presets."
      />
    </Page>
  );
}
