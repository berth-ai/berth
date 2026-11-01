import { useState } from "react";
import { refreshOrcaStatus, useOrcaStatus } from "@/api/queries";
import { ApiError, verb } from "@/api/request";
import { BusyButton } from "@/components/busy-button";
import { StatusBadge } from "@/components/status-badge";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { orcaView } from "./workspaces-logic";

/** OrcaFact says whether Orca has workspace id, with Sync now when it does
 * not: a project removed in Orca comes back only on the next sync. */
export function OrcaFact({ id }: { id: string }) {
  const status = useOrcaStatus(true);
  const [busy, setBusy] = useState(false);
  if (status.isError) {
    const down = status.error instanceof ApiError && status.error.code === "orca_unavailable";
    return (
      <span className="inline-flex flex-wrap items-center gap-2">
        <StatusBadge state="unknown" small />
        <span className="text-xs text-fg-muted">
          {down ? "Orca isn't answering yet." : errorText(status.error)}
        </span>
      </span>
    );
  }
  const view = orcaView(status.data, id);
  const sync = async () => {
    setBusy(true);
    try {
      await verb("orca.sync");
      await refreshOrcaStatus();
    } catch (e) {
      notify(errorText(e), "bad");
    } finally {
      setBusy(false);
    }
  };
  return (
    <span className="inline-flex flex-col gap-1">
      <span className="inline-flex flex-wrap items-center gap-2">
        <StatusBadge state={view.state} detail={view.detail} small />
        {view.canSync ? (
          <BusyButton
            size="sm"
            variant="outline"
            busy={busy}
            busyLabel="Syncing…"
            onClick={() => void sync()}
          >
            Sync now
          </BusyButton>
        ) : null}
      </span>
      {view.error ? (
        <span className="text-xs text-fg-muted">The last sync could not add it: {view.error}</span>
      ) : null}
    </span>
  );
}
