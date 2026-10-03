import { Page, PageHeader } from "@/components/page";
import { useTitle } from "@/lib/use-title";

export function ActivityPage() {
  useTitle("Activity");
  return (
    <Page>
      <PageHeader
        title="Activity"
        description="Jobs started from the panel since the container started."
      />
    </Page>
  );
}

export function JobPage({ id }: { id: number }) {
  useTitle("Activity");
  return (
    <Page>
      <PageHeader title={`Job ${id}`} />
    </Page>
  );
}
