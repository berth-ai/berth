import { Page, PageHeader } from "@/components/page";
import { useTitle } from "@/lib/use-title";

export function AccountsPage() {
  useTitle("Accounts");
  return (
    <Page narrow>
      <PageHeader
        title="Accounts"
        description="Your git identity and code-host sign-ins. Tokens stay in your home volume."
      />
    </Page>
  );
}
