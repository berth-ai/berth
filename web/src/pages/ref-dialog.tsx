import { useId, useState } from "react";
import { keys, queryClient } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { Job } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LogViewer } from "@/components/log-viewer";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { count } from "@/lib/format";
import { notify } from "@/lib/notify";
import { refError, repoLabel, type SkillRow } from "./skills-logic";

const BG = " Agents pick it up in the background.";

/** RefDialog moves a skill, or a whole source (whole), to another branch, tag
 * or commit. group is the skill's source; with more than one skill, a row's
 * dialog offers to move them all, ticked when defaultAll. */
export function RefDialog({
  row,
  group,
  whole = false,
  defaultAll = false,
  onClose,
}: {
  row: SkillRow;
  group: SkillRow[];
  whole?: boolean;
  defaultAll?: boolean;
  onClose: () => void;
}) {
  const source = repoLabel(row.skill.repo);
  const [ref, setRef] = useState(row.skill.ref);
  const [all, setAll] = useState(defaultAll);
  const [error, setError] = useState("");
  const [failed, setFailed] = useState("");
  const [job, setJob] = useState<Job | null>(null);
  const [busy, setBusy] = useState(false);
  const id = useId();
  const offerAll = !whole && group.length > 1;

  const save = async () => {
    const r = ref.trim();
    const e = refError(r);
    setError(e);
    setFailed("");
    if (e) return;
    const ids = whole || (offerAll && all) ? group.map((g) => g.id) : [row.id];
    setBusy(true);
    const finished = await runJob("skills.update", { ids, ref: r }, setJob);
    setBusy(false);
    void queryClient.invalidateQueries({ queryKey: keys.config });
    if (!finished) return;
    if (finished.state !== "succeeded") {
      const err = finished.result?.error;
      const msg = err?.message ?? "The job failed; open Activity for its log.";
      if (err?.field === "ref") setError(msg);
      else setFailed(msg);
      return;
    }
    const who = ids.length > 1 ? count(ids.length, "skill", "skills") : row.id;
    notify(
      finished.result?.changed === false ? "Already up to date." : `${who} moved to ${r}.${BG}`,
    );
    onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            Change ref for <span className="font-mono">{whole ? source : row.id}</span>
          </DialogTitle>
        </DialogHeader>
        <FormField
          id={id}
          label="Ref"
          hint={`${whole ? `Applies to all ${group.length} skills from this source. ` : ""}Berth fetches this branch, tag or commit and pins to the commit it points at.`}
          error={error}
        >
          <Input
            {...fieldProps(id, error)}
            value={ref}
            placeholder="main"
            className="font-mono text-xs"
            spellCheck={false}
            autoComplete="off"
            autoFocus
            onChange={(ev) => setRef(ev.target.value)}
            onKeyDown={(ev) => {
              if (ev.key === "Enter") void save();
            }}
          />
        </FormField>
        {offerAll ? (
          <Label className="flex items-start gap-2 leading-[inherit] font-normal">
            <Checkbox
              className="mt-0.5"
              checked={all}
              disabled={busy}
              onCheckedChange={(on) => setAll(on)}
            />
            Apply to all {group.length} skills from {source}
          </Label>
        ) : null}
        {failed ? <InlineAlert tone="bad">{failed}</InlineAlert> : null}
        {job?.log && busy ? <LogViewer text={job.log} truncated={job.log_truncated} /> : null}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <BusyButton busy={busy} busyLabel="Saving…" onClick={() => void save()}>
            Save
          </BusyButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
