import { Link, useNavigate } from "@tanstack/react-router";
import { TriangleAlert } from "lucide-react";
import { useEffect, useEffectEvent, useId, useRef, useState } from "react";
import { keys, queryClient, useConfig, useStatus } from "@/api/queries";
import { verb } from "@/api/request";
import { runJob } from "@/api/run-job";
import type { AgentPackage, DiffRow, Job, Source, SourceDiff } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { Facts, Mono } from "@/components/facts";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { LogViewer } from "@/components/log-viewer";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
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
import { errorText } from "@/lib/errors";
import { count } from "@/lib/format";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { cn } from "@/lib/utils";
import { applyAndNotify, BG } from "./apply-notify";
import { AttachPicker } from "./attach-picker";
import { AttachedSwitch } from "./attached-switch";
import { refError, repoLabel, shortCommit, skillAgents, unsupportedAgents } from "./skills-logic";
import {
  attachPatch,
  conflicts,
  detachPatch,
  diffOf,
  dropsAttached,
  exportPath,
  foundRows,
  missingNames,
  sourceResult,
  sourceSummary,
  updatedText,
  whoText,
} from "./sources-logic";

const mono = "font-mono text-xs";

/** attachedText is the toast after the Attach picker saved. */
function attachedText(name: string, next: string[] | null, agents: AgentPackage[]): string {
  if (next === null) return `${name} is attached to all agents.`;
  if (!next.length) return `${name} detached.`;
  return `${name} is attached to ${whoText({ enabled: true, agents: next }, agents)}.`;
}

/** SourcePage is one skill source: its facts, its last scan with who gets
 * each skill, the attachments an update left missing, and Update, Change
 * ref and Remove (DESIGN.md §4). */
export function SourcePage({ id }: { id: string }) {
  const config = useConfig();
  const status = useStatus();
  const navigate = useNavigate();
  const [picking, setPicking] = useState<string | null>(null);
  const [updating, setUpdating] = useState<"update" | "ref" | null>(null);
  const [removing, setRemoving] = useState(false);
  const [confirmDialog, confirm] = useConfirm();
  const sources = config.data?.config.skills?.sources;
  const source = sources?.[id];
  useTitle(source ? repoLabel(source.repo) : "Skill source");
  const packages = status.data?.packages ?? [];
  const agents = skillAgents(packages);
  const unsupported = unsupportedAgents(packages);

  if (!config.data)
    return (
      <Page>
        <LoadingRows n={3} />
      </Page>
    );
  if (!source)
    return (
      <Page>
        <PageHeader title="Skill source" />
        <EmptyState
          text={`There is no source named ${id}.`}
          action={
            <Link to="/skills" className="font-medium text-accent-text">
              Back to Skills
            </Link>
          }
        />
      </Page>
    );

  const missing = missingNames(source);
  const rows = foundRows(source).filter((r) => !missing.includes(r.name));
  const detach = (name: string) =>
    confirm({
      title: `Detach ${name}?`,
      body: "Berth removes it from every agent and deletes its copy.",
      confirm: "Detach",
      danger: true,
      action: () => applyAndNotify(detachPatch(id, name), `${name} detached.`),
    });

  return (
    <Page>
      <PageHeader
        title={repoLabel(source.repo)}
        description={`${id} · ${source.path || "the whole repository"} at ${source.ref}`}
        actions={
          <>
            <Button onClick={() => setUpdating("update")}>Update</Button>
            <Button variant="outline" onClick={() => setUpdating("ref")}>
              Change ref
            </Button>
            <Button variant="outline" onClick={() => setRemoving(true)}>
              Remove
            </Button>
          </>
        }
      />
      <PanelCard title="Source">
        <Facts
          rows={[
            ["Repository", <Mono key="repo">{source.repo}</Mono>],
            ["Ref", <Mono key="ref">{source.ref}</Mono>],
            ["Commit", <Mono key="commit">{source.commit}</Mono>],
            ["Folder", <Mono key="folder">{source.path || "/"}</Mono>],
            ["Store", <Mono key="store">{exportPath(id, "<skill>")}</Mono>],
          ]}
        />
      </PanelCard>
      {missing.length ? (
        <InlineAlert tone="warn">
          {missing.join(", ")} {missing.length === 1 ? "is" : "are"} attached but no longer in this
          source at {shortCommit(source.commit)}. Agents keep the copy they have until you detach{" "}
          {missing.length === 1 ? "it" : "them"}.
        </InlineAlert>
      ) : null}
      <PanelCard
        title="Skills"
        badge={<span className="text-xs text-fg-muted">{sourceSummary(source)}</span>}
      >
        <Table aria-label="Skills in this source">
          <TableHeader>
            <TableRow>
              <TableHead>Skill</TableHead>
              <TableHead>Attached to</TableHead>
              <TableHead>Enabled</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {missing.map((name) => {
              const attachment = source.attached?.[name] ?? { enabled: false };
              return (
                <TableRow key={`missing-${name}`}>
                  <TableCell data-label="">
                    <div className="flex min-w-0 flex-col gap-0.5">
                      <span className={cn(mono, "font-semibold text-fg")}>{name}</span>
                      <span className="inline-flex items-center gap-1 text-xs font-medium text-warn">
                        <TriangleAlert aria-hidden className="size-3" strokeWidth={2} />
                        Missing from {shortCommit(source.commit)}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell data-label="Attached to" className="text-xs text-fg-secondary">
                    {whoText(attachment, agents)}
                  </TableCell>
                  <TableCell data-label="Enabled">
                    <AttachedSwitch
                      id={id}
                      name={name}
                      attachment={attachment}
                      onApply={applyAndNotify}
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={`Detach ${name}`}
                      onClick={() => detach(name)}
                    >
                      Detach
                    </Button>
                  </TableCell>
                </TableRow>
              );
            })}
            {rows.map(({ name, found, attachment }) => (
              <TableRow key={name}>
                <TableCell data-label="">
                  <div
                    className={cn(
                      "flex min-w-0 flex-col gap-0.5",
                      attachment && !attachment.enabled && "opacity-70",
                    )}
                  >
                    <span className={cn(mono, "font-semibold text-fg")}>{name}</span>
                    <span className={cn("text-xs", found.error ? "text-warn" : "text-fg-muted")}>
                      {found.error ?? found.description}
                    </span>
                  </div>
                </TableCell>
                <TableCell
                  data-label="Attached to"
                  className={cn("text-xs", attachment ? "text-fg-secondary" : "text-fg-muted")}
                >
                  {found.error
                    ? "Can't be attached"
                    : attachment
                      ? whoText(attachment, agents)
                      : "Not attached"}
                </TableCell>
                <TableCell data-label="Enabled">
                  {attachment ? (
                    <AttachedSwitch
                      id={id}
                      name={name}
                      attachment={attachment}
                      onApply={applyAndNotify}
                    />
                  ) : null}
                </TableCell>
                <TableCell className="text-right">
                  {found.error ? null : (
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={`${attachment ? "Change who gets" : "Attach"} ${name}`}
                      onClick={() => setPicking(name)}
                    >
                      {attachment ? "Edit…" : "Attach…"}
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </PanelCard>
      {picking ? (
        <AttachPicker
          name={picking}
          source={id}
          attachment={source.attached?.[picking] ?? null}
          agents={agents}
          unsupported={unsupported}
          taken={conflicts(sources, id, picking, agents)}
          onSave={(next) =>
            applyAndNotify(
              attachPatch(id, picking, next, source.attached?.[picking] ?? null),
              attachedText(picking, next, agents),
            )
          }
          onClose={() => setPicking(null)}
        />
      ) : null}
      {updating ? (
        <UpdateDialog
          id={id}
          source={source}
          changeRef={updating === "ref"}
          onClose={() => setUpdating(null)}
        />
      ) : null}
      {removing ? (
        <RemoveSourceDialog
          id={id}
          source={source}
          onClose={() => setRemoving(false)}
          onRemoved={() => void navigate({ to: "/skills" })}
        />
      ) : null}
      {confirmDialog}
    </Page>
  );
}

/** UpdateDialog previews a source at its ref, or at a new one (changeRef),
 * shows what changes and applies exactly the previewed commit. */
export function UpdateDialog({
  id,
  source,
  changeRef,
  onClose,
}: {
  id: string;
  source: Source;
  changeRef: boolean;
  onClose: () => void;
}) {
  const [ref, setRef] = useState(source.ref);
  const [diff, setDiff] = useState<SourceDiff | null>(null);
  const [error, setError] = useState("");
  const [failed, setFailed] = useState("");
  const [job, setJob] = useState<Job | null>(null);
  const [busy, setBusy] = useState(false);
  const refId = useId();

  const preview = async () => {
    const r = ref.trim();
    const e = refError(r);
    setError(e);
    setFailed("");
    if (e) return;
    setBusy(true);
    const finished = await runJob("skills.sources.preview", { id, ref: r }, setJob);
    setBusy(false);
    if (!finished) return;
    const d = diffOf(finished);
    if (finished.state !== "succeeded" || !d) {
      setFailed(
        finished.result?.error?.message ?? "The preview failed; open Activity for its log.",
      );
      return;
    }
    setDiff(d);
    setJob(null);
  };

  const update = async () => {
    if (!diff) return;
    setBusy(true);
    const finished = await runJob(
      "skills.sources.update",
      { id, ref: diff.ref, commit: diff.commit },
      setJob,
    );
    setBusy(false);
    void queryClient.invalidateQueries({ queryKey: keys.config });
    if (!finished) return;
    if (finished.state !== "succeeded") {
      setFailed(finished.result?.error?.message ?? "The update failed; open Activity for its log.");
      return;
    }
    notify(updatedText(sourceResult(finished)) + BG);
    onClose();
  };

  // An Update previews once, on open; Change ref waits for the new ref.
  const previewed = useRef(false);
  const previewOnOpen = useEffectEvent(() => void preview());
  useEffect(() => {
    if (changeRef || previewed.current) return;
    previewed.current = true;
    previewOnOpen();
  }, [changeRef]);

  const lost = diff ? dropsAttached(diff) : [];
  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{changeRef ? `Change ref for ${id}` : `Update ${id}`}</DialogTitle>
        </DialogHeader>
        {changeRef && !diff ? (
          <FormField
            id={refId}
            label="Ref"
            hint="A branch, tag or commit. Berth shows what changes before it moves."
            error={error}
          >
            <Input
              {...fieldProps(refId, error)}
              value={ref}
              className={mono}
              spellCheck={false}
              autoComplete="off"
              onChange={(ev) => setRef(ev.target.value)}
            />
          </FormField>
        ) : null}
        {!changeRef && !diff && busy ? (
          <p className="text-fg-muted">Checking {source.ref} for new commits…</p>
        ) : null}
        {diff && !diff.changed ? (
          <p>
            Already up to date: {diff.ref} is still{" "}
            <code className={mono}>{shortCommit(diff.commit)}</code>.
          </p>
        ) : null}
        {diff?.changed ? <DiffView diff={diff} /> : null}
        {lost.length ? (
          <InlineAlert tone="warn">
            {lost.join(", ")} {lost.length === 1 ? "is" : "are"} attached and will be missing:
            agents keep their copy until you detach {lost.length === 1 ? "it" : "them"}.
          </InlineAlert>
        ) : null}
        {failed ? <InlineAlert tone="bad">{failed}</InlineAlert> : null}
        {job?.log ? <LogViewer text={job.log} truncated={job.log_truncated} /> : null}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {diff && !diff.changed ? "Close" : "Cancel"}
          </Button>
          {changeRef && !diff ? (
            <BusyButton busy={busy} busyLabel="Checking…" onClick={() => void preview()}>
              Preview
            </BusyButton>
          ) : null}
          {diff?.changed ? (
            <BusyButton busy={busy} busyLabel="Updating…" onClick={() => void update()}>
              Update to {shortCommit(diff.commit)}
            </BusyButton>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** DiffView is a preview's commit range and each kind of change. */
function DiffView({ diff }: { diff: SourceDiff }) {
  const kinds: [string, DiffRow[]][] = [
    ["Added", diff.added],
    ["Removed", diff.removed],
    ["Moved", diff.moved],
    ["Description changed", diff.described],
  ];
  const changes = kinds.filter(([, rows]) => rows.length);
  return (
    <div className="flex flex-col gap-3">
      {diff.from === diff.commit ? (
        <p>
          {diff.ref} is the commit Berth already has,{" "}
          <code className={mono}>{shortCommit(diff.commit)}</code>; Berth follows {diff.ref} from
          now on.
        </p>
      ) : (
        <p>
          {diff.ref}: <code className={mono}>{shortCommit(diff.from)}</code> →{" "}
          <code className={mono}>{shortCommit(diff.commit)}</code>
        </p>
      )}
      {changes.length ? (
        changes.map(([title, rows]) => (
          <section key={title} aria-label={title} className="flex flex-col gap-1">
            <h3 className="text-xs font-medium text-fg-muted">
              {title} ({rows.length})
            </h3>
            <ul className="flex flex-col">
              {rows.map((r) => (
                <li
                  key={r.name}
                  className="flex flex-wrap items-baseline gap-x-2 border-b border-line py-1.5 last:border-b-0"
                >
                  <span className={cn(mono, "font-semibold text-fg")}>{r.name}</span>
                  {r.attached ? (
                    <span className="text-xs font-medium text-accent-text">attached</span>
                  ) : null}
                  <span className={cn("text-xs", r.error ? "text-warn" : "text-fg-muted")}>
                    {r.from ? `${r.from} → ${r.path}` : (r.error ?? r.description ?? r.path)}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        ))
      ) : (
        <p className="text-fg-muted">No skill changed between these commits.</p>
      )}
    </div>
  );
}

/** RemoveSourceDialog removes a source; with skills attached, the user
 * types its id first. */
export function RemoveSourceDialog({
  id,
  source,
  onClose,
  onRemoved,
}: {
  id: string;
  source: Source;
  onClose: () => void;
  onRemoved: () => void;
}) {
  const attached = Object.keys(source.attached ?? {}).length;
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState("");
  const inputId = useId();

  const remove = async () => {
    setBusy(true);
    try {
      await verb("skills.sources.remove", { id });
      notify(`${id} removed.${BG}`);
      await queryClient.invalidateQueries({ queryKey: keys.config });
      onRemoved();
    } catch (err) {
      setFailed(errorText(err));
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Remove {id}?</DialogTitle>
        </DialogHeader>
        <p>
          {attached
            ? `Berth detaches its ${count(attached, "skill", "skills")} from every agent and deletes their copies.`
            : "Nothing from it is attached; Berth forgets the source."}
        </p>
        {attached ? (
          <FormField id={inputId} label={`Type ${id} to confirm`}>
            <Input
              {...fieldProps(inputId, undefined)}
              value={typed}
              className={mono}
              spellCheck={false}
              autoComplete="off"
              onChange={(ev) => setTyped(ev.target.value)}
            />
          </FormField>
        ) : null}
        {failed ? <InlineAlert tone="bad">{failed}</InlineAlert> : null}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <BusyButton
            variant="destructive"
            busy={busy}
            busyLabel="Removing…"
            disabled={attached > 0 && typed.trim() !== id}
            onClick={() => void remove()}
          >
            Remove source
          </BusyButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
