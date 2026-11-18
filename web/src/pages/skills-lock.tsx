import { keys, queryClient, refreshWorkspaceReport, useSkillsLock } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { LockAddResult } from "@/api/types";
import { useConfirm } from "@/components/confirm";
import { InlineAlert } from "@/components/inline-alert";
import { PanelCard } from "@/components/panel-card";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorText } from "@/lib/errors";
import { count } from "@/lib/format";
import { notify } from "@/lib/notify";
import { cn } from "@/lib/utils";
import { addableNames, lockAddedText, lockOrigin, lockState } from "./skills-lock-logic";

const mono = "font-mono text-xs";
const TONE = { ok: "text-ok", warn: "text-warn", info: "text-info", idle: "text-fg-muted" };

/** SkillsLockCard shows what workspace id's skills-lock.json (from the npx
 * skills CLI) asks for, and offers to add the skills the workspace lacks:
 * each from a source that has it, else from a new source pinned to the
 * lock's commit, linked into the workspace for its agents. Nothing is
 * shown when the workspace has no lock file. */
export function SkillsLockCard({ id, readOnly }: { id: string; readOnly: boolean }) {
  const lock = useSkillsLock(id);
  const [confirmDialog, confirm] = useConfirm();
  if (lock.error)
    return <InlineAlert tone="warn">skills-lock.json: {errorText(lock.error)}</InlineAlert>;
  if (!lock.data?.found) return null;
  const rows = lock.data.skills;
  const names = addableNames(rows);
  const add = () =>
    confirm({
      title: `Add ${count(names.length, "skill", "skills")} to ${id}?`,
      body: (
        <div className="flex flex-col gap-2">
          <p>
            Berth links {names.join(", ")} into {id} for its agents. A skill no source has yet comes
            from a new source pinned to the commit the lock names.
          </p>
          <p className="text-xs text-fg-muted">
            Skills are instructions and scripts your agents run. Add only what you trust.
          </p>
        </div>
      ),
      confirm: "Add",
      action: async () => {
        const job = await runJob("workspaces.skills_lock.add", { id, names });
        void queryClient.invalidateQueries({ queryKey: keys.config });
        void queryClient.invalidateQueries({ queryKey: keys.skillsLock(id) });
        void refreshWorkspaceReport(id);
        if (!job) return false;
        const r = (job.result ?? {}) as LockAddResult;
        if (job.state !== "succeeded") {
          notify(r.error?.message ?? "The job failed; open Activity for its log.", "bad");
          return false;
        }
        notify(lockAddedText(r, id), r.skipped?.length ? "info" : undefined);
      },
    });
  return (
    <PanelCard
      title="skills-lock.json"
      badge={<span className="text-xs text-fg-muted">{count(rows.length, "skill", "skills")}</span>}
      actions={
        names.length ? (
          <Button size="sm" disabled={readOnly} onClick={add}>
            Add {count(names.length, "skill", "skills")}
          </Button>
        ) : null
      }
      footer={
        <span className="text-xs text-fg-muted">
          The skills this repository pins with the npx skills CLI.
        </span>
      }
    >
      <Table aria-label="Skills in skills-lock.json">
        <TableHeader>
          <TableRow>
            <TableHead>Skill</TableHead>
            <TableHead>State</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((s) => {
            const st = lockState(s);
            return (
              <TableRow key={s.name}>
                <TableCell data-label="">
                  <div className="flex min-w-0 flex-col gap-0.5">
                    <span className={cn(mono, "font-semibold text-fg")}>{s.name}</span>
                    <span className={cn(mono, "break-all text-fg-muted")}>{lockOrigin(s)}</span>
                  </div>
                </TableCell>
                <TableCell data-label="State" className={cn("text-xs font-medium", TONE[st.tone])}>
                  {st.label}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
      {confirmDialog}
    </PanelCard>
  );
}
