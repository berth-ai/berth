import { Button } from "@/components/ui/button";
import { BusyButton } from "./busy-button";

/** SaveBar is the sticky "Unsaved changes" region (DESIGN.md §4). */
export function SaveBar({
  summary,
  busy,
  onSave,
  onDiscard,
}: {
  summary: string;
  busy: boolean;
  onSave: () => void;
  onDiscard: () => void;
}) {
  return (
    <div
      role="region"
      aria-label="Unsaved changes"
      className="sticky bottom-4 z-10 flex flex-wrap items-center gap-2 rounded-xl border border-line-strong bg-surface py-2.5 pr-3 pl-4 shadow-md max-sm:-mx-4 max-sm:rounded-none"
    >
      <strong className="font-semibold">Unsaved changes</strong>
      <span className="text-fg-secondary">{summary}</span>
      <span className="flex-1" />
      <Button variant="outline" onClick={onDiscard}>
        Discard
      </Button>
      <BusyButton busy={busy} busyLabel="Saving…" onClick={onSave}>
        Save
      </BusyButton>
    </div>
  );
}
