import { useState } from "react";
import { runJob } from "@/api/run-job";
import type { Job, RegistryAgent } from "@/api/types";
import { BrandIcon } from "@/components/brand-icon";
import { BusyButton } from "@/components/busy-button";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { LogViewer } from "@/components/log-viewer";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

/** AddAgentDialog lists the image's registry. Choosing an entry runs
 * agents.add and shows its log; success closes the dialog. */
export function AddAgentDialog({
  entries,
  failed,
  onClose,
  onAdded,
}: {
  entries: RegistryAgent[] | undefined;
  failed: string;
  onClose: () => void;
  onAdded: (name: string) => void;
}) {
  const [busy, setBusy] = useState<string | null>(null);
  const [job, setJob] = useState<Job | null>(null);
  const [error, setError] = useState("");

  const add = async (e: RegistryAgent) => {
    setBusy(e.id);
    setError("");
    setJob(null);
    const done = await runJob("agents.add", { id: e.id }, setJob);
    setBusy(null);
    if (done?.state === "succeeded") {
      onAdded(e.name);
      onClose();
      return;
    }
    if (done) setError(done.result?.error?.message ?? "The install failed; read the log below.");
  };

  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Add an agent</DialogTitle>
        </DialogHeader>
        {failed ? (
          <InlineAlert tone="bad">{failed}</InlineAlert>
        ) : !entries ? (
          <LoadingRows n={2} />
        ) : (
          <ul className="flex flex-col divide-y" aria-label="Registry">
            {entries.map((e) => (
              <li key={e.id} className="flex items-start gap-3 py-3">
                <BrandIcon slug={e.icon} name={e.name} id={e.package} />
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <strong className="text-fg">{e.name}</strong>
                    <span className="font-mono text-xs text-fg-muted">{e.version}</span>
                    {e.added ? <StatusBadge small state="added" /> : null}
                  </div>
                  <span className="text-xs text-fg-muted">{e.description}</span>
                  <span className="text-xs text-fg-muted">
                    Sign-in: {e.login?.length ? e.login.join(", ") : "none"} · Keys:{" "}
                    {e.keys?.length ? (
                      <span className="font-mono">{e.keys.join(", ")}</span>
                    ) : (
                      "none"
                    )}
                  </span>
                </div>
                <BusyButton
                  size="sm"
                  variant="outline"
                  busy={busy === e.id}
                  busyLabel="Adding…"
                  disabled={e.added || (busy !== null && busy !== e.id)}
                  aria-label={`Add ${e.name}`}
                  onClick={() => void add(e)}
                >
                  Add
                </BusyButton>
              </li>
            ))}
          </ul>
        )}
        <div aria-live="polite">{error ? <InlineAlert tone="bad">{error}</InlineAlert> : null}</div>
        {job?.log ? <LogViewer text={job.log} truncated={job.log_truncated} /> : null}
        <DialogFooter>
          <Button variant="outline" disabled={busy !== null} onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
