import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useState } from "react";
import {
  keys,
  queryClient,
  refreshWorkspaceReport,
  useConfig,
  useWorkspaceReport,
  useWorkspaceStatus,
} from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { SkillMode } from "@/api/types";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { PanelCard } from "@/components/panel-card";
import { Segmented } from "@/components/segmented";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { count } from "@/lib/format";
import { notify } from "@/lib/notify";
import { cn } from "@/lib/utils";
import { applyAndNotify } from "./apply-notify";
import {
  type FindRow,
  findable,
  MODE_LABELS,
  resolvedText,
  type SkillState,
  skillState,
  strandedSkills,
  workspaceSkills,
  wsPatch,
} from "./attachments-logic";
import type { Sources } from "./sources-logic";

const mono = "font-mono text-xs";
const TONE: Record<SkillState["tone"], string> = {
  ok: "text-ok",
  warn: "text-warn",
  info: "text-info",
  idle: "text-fg-muted",
};
const MODES = (["link", "vendor"] as const).map((m) => ({ value: m, label: MODE_LABELS[m] }));
const placed = (mode: SkillMode, name: string, id: string) =>
  `${name} is ${mode === "vendor" ? "vendored into" : "linked into"} ${id}.`;

/** WorkspaceSkills is a workspace's Skills tab (DESIGN.md §4): every skill
 * attached to it from any source, how it gets each one and what Berth
 * did, with Overwrite and Stop managing for a vendored copy with local
 * edits, and Attach skill. */
export function WorkspaceSkills({ id }: { id: string }) {
  const config = useConfig();
  const report = useWorkspaceReport(id);
  const states = useWorkspaceStatus();
  const [attaching, setAttaching] = useState(false);
  const [confirmDialog, confirm] = useConfirm();
  if (!config.data) return <LoadingRows n={2} />;
  const sources = config.data.config.skills?.sources;
  const rows = workspaceSkills(sources, id);
  const stranded = strandedSkills(report.data, rows);
  const st = states.data?.workspaces[id];
  const readOnly = Boolean(st?.exists && !st.writable);
  const apply = async (patch: object, done: string) => {
    await applyAndNotify(patch, done);
    void refreshWorkspaceReport(id);
  };
  const resolve = (sourceId: string, name: string, action: "overwrite" | "release") =>
    confirm({
      title: action === "overwrite" ? `Overwrite ${name}?` : `Stop managing ${name}?`,
      body:
        action === "overwrite"
          ? `Berth replaces .agents/skills/${name} in ${id} with the source's copy, or removes it if it is detached. Your local edits are lost.`
          : `Berth forgets ${name} in ${id}: its files and links stay in the repository as yours, and the skill is detached from this workspace.`,
      confirm: action === "overwrite" ? "Overwrite" : "Stop managing",
      danger: action === "overwrite",
      action: async () => {
        const done = await runJob("skills.vendor.resolve", {
          source: sourceId,
          name,
          workspace: id,
          action,
        });
        if (done?.state !== "succeeded") {
          if (done)
            notify(
              done.result?.error?.message ?? "The job failed; open Activity for its log.",
              "bad",
            );
          return false;
        }
        notify(resolvedText(action, name, id));
        void refreshWorkspaceReport(id);
        void queryClient.invalidateQueries({ queryKey: keys.config });
      },
    });
  const editedActions = (sourceId: string, name: string) => (
    <>
      <Button
        variant="outline"
        size="sm"
        aria-label={`Overwrite ${name}`}
        onClick={() => resolve(sourceId, name, "overwrite")}
      >
        Overwrite
      </Button>
      <Button
        variant="outline"
        size="sm"
        aria-label={`Stop managing ${name}`}
        onClick={() => resolve(sourceId, name, "release")}
      >
        Stop managing
      </Button>
    </>
  );

  return (
    <>
      {readOnly ? (
        <InlineAlert tone="warn">
          Berth can't write here: skills, MCP and instructions can't be attached until agent can
          write this folder.
        </InlineAlert>
      ) : null}
      {report.data?.skills.error ? (
        <InlineAlert tone="bad">{report.data.skills.error}</InlineAlert>
      ) : null}
      <PanelCard
        title="Skills"
        badge={
          <span className="text-xs text-fg-muted">{count(rows.length, "skill", "skills")}</span>
        }
        actions={
          <Button size="sm" disabled={readOnly} onClick={() => setAttaching(true)}>
            <Plus aria-hidden /> Attach skill…
          </Button>
        }
      >
        {rows.length || stranded.length ? (
          <Table aria-label="Skills in this workspace">
            <TableHeader>
              <TableRow>
                <TableHead>Skill</TableHead>
                <TableHead>Mode</TableHead>
                <TableHead>State</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((r) => {
                const s = skillState(r, report.data);
                return (
                  <TableRow key={r.key}>
                    <TableCell data-label="">
                      <div className="flex min-w-0 flex-col gap-0.5">
                        <span className={cn(mono, "font-semibold text-fg")}>{r.name}</span>
                        <Link
                          to="/skills/$id"
                          params={{ id: r.sourceId }}
                          className="text-xs text-accent-text"
                        >
                          {r.sourceId}
                        </Link>
                      </div>
                    </TableCell>
                    <TableCell data-label="Mode">
                      <Segmented
                        size="sm"
                        label={`How ${id} gets ${r.name}`}
                        value={r.mode}
                        options={MODES}
                        className="w-36"
                        onChange={(m) =>
                          void apply(
                            wsPatch(r.sourceId, r.name, id, m, r.attachment),
                            placed(m, r.name, id),
                          )
                        }
                      />
                    </TableCell>
                    <TableCell data-label="State">
                      <div className="flex flex-col gap-0.5 text-xs">
                        <span className={cn("font-medium", TONE[s.tone])}>{s.label}</span>
                        {s.detail ? (
                          <span className={cn(mono, "break-all text-fg-muted")}>{s.detail}</span>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="inline-flex flex-wrap justify-end gap-2">
                        {s.edited ? editedActions(r.sourceId, r.name) : null}
                        <Button
                          variant="outline"
                          size="sm"
                          aria-label={`Detach ${r.name}`}
                          onClick={() =>
                            confirm({
                              title: `Detach ${r.name} from ${id}?`,
                              body:
                                r.mode === "vendor"
                                  ? "Berth removes the copy and its links from the repository, unless you edited it."
                                  : "Berth removes its links from the repository.",
                              confirm: "Detach",
                              danger: true,
                              action: () =>
                                apply(
                                  wsPatch(r.sourceId, r.name, id, null, r.attachment),
                                  `${r.name} detached from ${id}.`,
                                ),
                            })
                          }
                        >
                          Detach
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
              {stranded.map((r) => (
                <TableRow key={r.key}>
                  <TableCell data-label="">
                    <span className={cn(mono, "font-semibold text-fg")}>{r.entry.name}</span>
                  </TableCell>
                  <TableCell data-label="Mode" className="text-xs">
                    {MODE_LABELS[r.entry.mode]}
                  </TableCell>
                  <TableCell data-label="State">
                    <div className="flex flex-col gap-0.5 text-xs">
                      <span className="font-medium text-warn">Detached, local edits</span>
                      <span className={cn(mono, "text-fg-muted")}>{r.entry.dir}</span>
                    </div>
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="inline-flex flex-wrap justify-end gap-2">
                      {editedActions(r.sourceId, r.entry.name)}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyState text="No skills in this workspace yet. Attach one from any source." />
        )}
      </PanelCard>
      {attaching ? (
        <AttachSkillDialog id={id} sources={sources} onClose={() => setAttaching(false)} />
      ) : null}
      {confirmDialog}
    </>
  );
}

/** AttachSkillDialog searches every source's skills and attaches one to
 * workspace id, linked or vendored. A skill another source already gives
 * the workspace is shown with that source and cannot be picked. */
export function AttachSkillDialog({
  id,
  sources,
  onClose,
}: {
  id: string;
  sources: Sources;
  onClose: () => void;
}) {
  const [query, setQuery] = useState("");
  const [mode, setMode] = useState<SkillMode>("link");
  const [busy, setBusy] = useState("");
  const none = !Object.keys(sources ?? {}).length;
  const rows = findable(sources, id, query);
  const attach = async (r: FindRow) => {
    setBusy(`${r.sourceId}/${r.name}`);
    const ok = await applyAndNotify(
      wsPatch(r.sourceId, r.name, id, mode, r.source.attached?.[r.name] ?? null),
      placed(mode, r.name, id),
    );
    setBusy("");
    if (ok) {
      void refreshWorkspaceReport(id);
      onClose();
    }
  };
  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{`Attach a skill to ${id}`}</DialogTitle>
        </DialogHeader>
        {none ? (
          <EmptyState
            text="No skill sources yet. Add one on the Skills page, then attach its skills here."
            action={
              <Link to="/skills" className="font-medium text-accent-text">
                Open Skills
              </Link>
            }
          />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <Input
                aria-label="Search skills"
                placeholder="Search skills"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                className="min-w-0 flex-1"
              />
              <Segmented
                label="Mode"
                value={mode}
                options={MODES}
                onChange={setMode}
                className="w-40"
              />
            </div>
            <p className="text-xs text-fg-muted">
              Link keeps the skill in Berth's store and hides the link from git; it works only
              inside the container. Vendor copies the files into the repository for you to commit.
            </p>
            <ul
              aria-label="Skills"
              className="flex max-h-80 flex-col divide-y divide-line overflow-y-auto"
            >
              {rows.map((r) => (
                <li
                  key={`${r.sourceId}/${r.name}`}
                  className="flex items-start justify-between gap-3 py-2"
                >
                  <div className="flex min-w-0 flex-col gap-0.5">
                    <span className={cn(mono, "font-semibold text-fg")}>{r.name}</span>
                    <span className="text-xs text-fg-muted">
                      {r.sourceId} · {r.found.description}
                    </span>
                  </div>
                  {r.attached ? (
                    <span className="shrink-0 text-xs text-fg-muted">Attached</span>
                  ) : r.taken ? (
                    <span className="shrink-0 text-xs text-fg-muted">From {r.taken}</span>
                  ) : (
                    <Button
                      size="sm"
                      variant="outline"
                      className="shrink-0"
                      aria-label={`Attach ${r.name} from ${r.sourceId}`}
                      disabled={busy !== ""}
                      onClick={() => void attach(r)}
                    >
                      {busy === `${r.sourceId}/${r.name}` ? "Attaching…" : "Attach"}
                    </Button>
                  )}
                </li>
              ))}
              {rows.length ? null : (
                <li className="py-2 text-xs text-fg-muted">No skill matches.</li>
              )}
            </ul>
          </>
        )}
        <DialogFooter>
          <Button variant="outline" disabled={busy !== ""} onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
