import { Page, PageHeader } from "@/components/page";
import { useTitle } from "@/lib/use-title";

export function SettingsPage() {
  useTitle("Settings");
  return (
    <Page narrow>
      <PageHeader title="Settings" description="Changes apply without restarting the container." />
    </Page>
  );
}
