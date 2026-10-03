import { refreshStatus } from "@/api/queries";
import { verb } from "@/api/request";
import type { ApplyResult, Status } from "@/api/types";
import { useConfirm } from "@/components/confirm";
import { InlineAlert } from "@/components/inline-alert";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { Button } from "@/components/ui/button";
import { errorText } from "@/lib/errors";
import { describeChanges } from "@/lib/format";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";

// Recovery mode: the stored config cannot be read. Only the panel runs.
export function RecoveryPage({ status }: { status: Status | undefined }) {
  useTitle("Recovery");
  const [dialog, confirm] = useConfirm();
  const act = (title: string, name: string, body: string, label: string, danger?: boolean) => () =>
    confirm({
      title,
      body,
      confirm: label,
      danger,
      action: async () => {
        try {
          const r = await verb<ApplyResult>(name);
          notify(describeChanges(r.changes));
          void refreshStatus();
          return true;
        } catch (err) {
          notify(errorText(err), "bad");
          return false;
        }
      },
    });
  return (
    <Page narrow>
      <PageHeader
        title="Berth needs attention"
        description="The settings file cannot be read, so only the panel is running."
      />
      <PanelCard
        title="Settings could not be read"
        footer={
          <>
            <Button
              onClick={act(
                "Revert to the previous config?",
                "config.revert",
                "Berth applies the revision before the current one and starts the services it enables.",
                "Revert",
              )}
            >
              Revert to previous config
            </Button>
            <Button
              variant="outline"
              onClick={act(
                "Reset every setting to its default?",
                "config.reset",
                "Frontends, agent settings and the time zone return to their defaults. Your login, API keys, tools and files are kept.",
                "Reset to defaults",
                true,
              )}
            >
              Reset to defaults
            </Button>
          </>
        }
      >
        <InlineAlert tone="bad">
          {status?.config_error || "The configuration is unusable."}
        </InlineAlert>
        <p>
          Revert to the previous revision if you changed something recently. Otherwise reset to the
          defaults. The unreadable file is kept as config.corrupt.json for inspection.
        </p>
      </PanelCard>
      {dialog}
    </Page>
  );
}
