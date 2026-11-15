import { Link, useNavigate } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useState } from "react";
import { keys, queryClient, useConfig, useJobs, useSkillsCheck, useStatus } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { AgentPackage, Source, SourceCheck } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { Segmented } from "@/components/segmented";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { count } from "@/lib/format";
import { notify, type Tone } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { cn } from "@/lib/utils";
import { applyAndNotify, BG } from "./apply-notify";
import { AttachedSwitch } from "./attached-switch";
import { targetsText } from "./attachments-logic";
import { UpdateDialog } from "./source";
import { SourceDialog, type SourceStart } from "./source-dialog";
import { repoLabel, shortCommit, skillAgents, type SkillView } from "./skills-logic";
import {
  type AttachedRow,
  attachedRows,
  behindSources,
  bulkResult,
  changeCounts,
  checkState,
  checkSummary,
  checksOf,
  dropsAttached,
  hasSuggested,
  missingNames,
  SUGGESTED,
  type SourceEntry,
  sourceList,
  sourceSummary,
  updatedAllText,
} from "./sources-logic";

const VIEWS = [
  { value: "source", label: "By source" },
  { value: "flat", label: "All skills" },
] as const;
const mono = "font-mono text-xs";

// Skills: repositories of skills, pinned to a commit, attached skill by skill.
export function SkillsPage({ view, onView }: { view: SkillView; onView: (v: SkillView) => void }) {
  useTitle("Skills");
  const status = useStatus();
  const config = useConfig();
  const jobs = useJobs();
  const [adding, setAdding] = useState<SourceStart | true | null>(null);
  const navigate = useNavigate();
  const agents = skillAgents(status.data?.packages ?? []);
  const sources = config.data?.config.skills?.sources;
  const list = config.data ? sourceList(sources) : undefined;
  const attached = config.data ? attachedRows(sources) : undefined;
  const suggest = config.data ? !hasSuggested(sources) : false;
  const lastSync = (jobs.data?.jobs ?? []).find((j) => j.verb === "skills.sync");
  const checks = useSkillsCheck().data;
  const behind = behindSources(sources, checks);
  const [checking, setChecking] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [result, setResult] = useState<{ tone: Tone; text: string } | null>(null);
  const [confirmDialog, confirm] = useConfirm();

  const check = async () => {
    setChecking(true);
    setResult(null);
    const job = await runJob("skills.sources.check", {});
    setChecking(false);
    if (!job) return;
    const found = checksOf(job);
    if (job.state !== "succeeded" || !found) {
      setResult({
        tone: "bad",
        text: job.result?.error?.message ?? "The check failed; open Activity for its log.",
      });
      return;
    }
    queryClient.setQueryData(keys.skillsCheck, found);
    setResult(checkSummary(found));
  };

  const updateAll = (list: SourceCheck[]) =>
    confirm({
      title: `Update ${count(list.length, "source", "sources")}?`,
      body: <UpdateAllList list={list} />,
      confirm: "Update all",
      action: async () => {
        setUpdating(true);
        setResult(null);
        notify("Updating; follow it in Activity.", "info");
        // The commits the dialog listed, not whatever is newest by then.
        const updates = list.map((c) => ({ id: c.id, ref: c.ref, commit: c.commit }));
        void runJob("skills.sources.update_all", { updates }).then((job) => {
          setUpdating(false);
          void queryClient.invalidateQueries({ queryKey: keys.config });
          if (!job) return;
          const r = bulkResult(job);
          if (job.state === "succeeded") notify(updatedAllText(r) + (r.changed ? BG : ""));
          else if (r.updated || r.failed) notify(updatedAllText(r), "bad");
          else notify(r.error?.message ?? "The update failed; open Activity for its log.", "bad");
        });
      },
    });

  let body;
  if (!list || !attached) body = <LoadingRows n={2} />;
  else if (view === "flat")
    body = attached.length ? (
      <AttachedTable rows={attached} agents={agents} />
    ) : (
      <EmptyState text="No skills attached yet. Open a source to attach its skills." />
    );
  else
    body = (
      <>
        {list.length ? null : (
          <EmptyState text="No skill sources yet. Add a repository of skills, or start with the suggested one." />
        )}
        <SourceTable
          list={list}
          checks={checks}
          suggest={suggest}
          onSuggested={() =>
            setAdding({
              repo: SUGGESTED.repo,
              ref: SUGGESTED.ref,
              path: SUGGESTED.path,
              find: true,
            })
          }
        />
      </>
    );

  return (
    <Page>
      <PageHeader
        title="Skills"
        description="Folders with a SKILL.md that agents load when a task calls for them. Add a repository as a source, then attach its skills to agents. Berth pins each source to a commit and updates it only when you ask."
      />
      {lastSync?.state === "failed" ? (
        <InlineAlert
          tone="bad"
          action={
            <Link
              to="/activity/$id"
              params={{ id: String(lastSync.id) }}
              className="font-medium text-accent-text"
            >
              Open its log
            </Link>
          }
        >
          The last skills update failed; some agents may have an older copy.
        </InlineAlert>
      ) : null}
      <div aria-live="polite">
        {result ? <InlineAlert tone={result.tone}>{result.text}</InlineAlert> : null}
      </div>
      <PanelCard
        title={view === "flat" ? "All skills" : "Sources"}
        badge={
          list && attached ? (
            <span className="text-xs text-fg-muted">
              {count(list.length, "source", "sources")} · {attached.length} attached
            </span>
          ) : null
        }
        actions={
          <div className="flex flex-wrap items-center gap-2.5">
            <Segmented label="View" value={view} options={VIEWS} onChange={onView} size="sm" />
            {list?.length ? (
              <BusyButton
                variant="outline"
                size="sm"
                busy={checking}
                busyLabel="Checking…"
                disabled={updating}
                onClick={() => void check()}
              >
                Check for updates
              </BusyButton>
            ) : null}
            {behind.length ? (
              <BusyButton
                variant="outline"
                size="sm"
                busy={updating}
                busyLabel="Updating…"
                disabled={checking}
                onClick={() => updateAll(behind)}
              >
                Update all ({behind.length})
              </BusyButton>
            ) : null}
            <Button size="sm" onClick={() => setAdding(true)}>
              <Plus aria-hidden /> Add source
            </Button>
          </div>
        }
      >
        {body}
      </PanelCard>
      {adding ? (
        <SourceDialog
          start={adding === true ? undefined : adding}
          onClose={() => setAdding(null)}
          onAdded={(id) => {
            setAdding(null);
            void navigate({ to: "/skills/$id", params: { id } });
          }}
        />
      ) : null}
      {confirmDialog}
    </Page>
  );
}

/** UpdateAllList is what Update all moves: each source's commits and
 * changes, and the attached skills it leaves missing. */
function UpdateAllList({ list }: { list: SourceCheck[] }) {
  const lost = list.flatMap((c) => dropsAttached(c).map((name) => `${name} (${c.id})`));
  return (
    <div className="flex flex-col gap-3">
      <ul className="flex flex-col" aria-label="Sources to update">
        {list.map((c) => (
          <li
            key={c.id}
            className="flex flex-wrap items-baseline gap-x-2 border-b border-line py-1.5 last:border-b-0"
          >
            <span className={cn(mono, "font-semibold text-fg")}>{c.id}</span>
            <span className={cn(mono, "text-fg-muted")}>
              {c.ref}: {shortCommit(c.from)} → {shortCommit(c.commit)}
            </span>
            <span className="text-xs text-fg-secondary">
              {changeCounts(c) || "No skill changed"}
            </span>
          </li>
        ))}
      </ul>
      {lost.length ? (
        <InlineAlert tone="warn">
          {lost.join(", ")} {lost.length === 1 ? "is" : "are"} attached and will be missing: agents
          keep their copy until you detach {lost.length === 1 ? "it" : "them"}.
        </InlineAlert>
      ) : null}
      <p className="text-xs text-fg-muted">
        Open a source and use Update to see each skill that changes.
      </p>
    </div>
  );
}

/** CheckNote is what the last check found for a source, under its ref. */
function CheckNote({ source, check }: { source: Source; check: SourceCheck | undefined }) {
  const st = checkState(source, check);
  if (!st) return null;
  if (st.kind === "current") return <span className="text-xs text-fg-muted">Up to date</span>;
  if (st.kind === "failed")
    return (
      <span className="text-xs text-warn" title={st.message}>
        Could not check
      </span>
    );
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      <StatusBadge state="update" small />
      <span className={cn(mono, "text-fg-muted")}>{shortCommit(st.check.commit)}</span>
    </span>
  );
}

/** SourceTable lists the sources, then the Suggested row. */
function SourceTable({
  list,
  checks,
  suggest,
  onSuggested,
}: {
  list: SourceEntry[];
  checks: SourceCheck[] | undefined;
  suggest: boolean;
  onSuggested: () => void;
}) {
  return (
    <Table aria-label="Skill sources">
      <TableHeader>
        <TableRow>
          <TableHead>Source</TableHead>
          <TableHead>Ref</TableHead>
          <TableHead>Skills</TableHead>
          <TableHead>
            <span className="sr-only">Actions</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {list.map(({ id, source }) => (
          <SourceRow key={id} id={id} source={source} check={checks?.find((c) => c.id === id)} />
        ))}
        {suggest ? (
          <TableRow className="bg-surface-sunken hover:bg-surface-sunken">
            <TableCell data-label="">
              <div className="flex min-w-0 flex-col gap-0.5">
                <span className={cn(mono, "font-semibold text-fg")}>{SUGGESTED.label}</span>
                <span className="text-xs text-fg-muted">{SUGGESTED.description}</span>
              </div>
            </TableCell>
            <TableCell data-label="Ref">
              <span className={cn(mono, "text-fg-secondary")}>{SUGGESTED.ref}</span>
            </TableCell>
            <TableCell data-label="Skills" className="text-xs text-fg-muted">
              Suggested
            </TableCell>
            <TableCell className="text-right">
              <Button
                variant="outline"
                size="sm"
                aria-label={`Add ${SUGGESTED.label}`}
                onClick={onSuggested}
              >
                Add
              </Button>
            </TableCell>
          </TableRow>
        ) : null}
      </TableBody>
    </Table>
  );
}

/** SourceRow is one source: repository and folder, ref and commit, counts.
 * The repository links to the source's page, and so does a click anywhere
 * else on the row. */
function SourceRow({
  id,
  source,
  check,
}: {
  id: string;
  source: Source;
  check: SourceCheck | undefined;
}) {
  const missing = missingNames(source).length;
  const [updating, setUpdating] = useState(false);
  const navigate = useNavigate();
  return (
    <TableRow
      className="cursor-pointer"
      onClick={(e) => {
        // Clicks from the Update dialog's portal bubble here too.
        const target = e.target as HTMLElement;
        if (!e.currentTarget.contains(target) || target.closest("a, button")) return;
        if (window.getSelection()?.toString()) return;
        void navigate({ to: "/skills/$id", params: { id } });
      }}
    >
      <TableCell data-label="">
        <div className="flex min-w-0 flex-col gap-0.5">
          <Link
            to="/skills/$id"
            params={{ id }}
            className={cn(
              mono,
              "w-fit font-semibold break-all text-accent-text hover:text-accent-hover",
            )}
          >
            {repoLabel(source.repo)}
          </Link>
          <span className={cn(mono, "text-fg-muted")}>
            {id} · {source.path || "/"}
          </span>
        </div>
      </TableCell>
      <TableCell data-label="Ref">
        <div className="flex flex-col gap-1">
          <span className="flex flex-wrap gap-x-1.5 font-mono text-xs" title={source.commit}>
            <span className="break-all text-fg-secondary">{source.ref}</span>
            <span className="text-fg-muted">{shortCommit(source.commit)}</span>
          </span>
          <CheckNote source={source} check={check} />
        </div>
      </TableCell>
      <TableCell data-label="Skills" className="text-xs text-fg-secondary">
        {sourceSummary(source)}
        {missing ? <span className="text-warn"> · {missing} missing</span> : null}
      </TableCell>
      <TableCell className="text-right">
        <Button
          variant="outline"
          size="sm"
          aria-label={`Update ${id}`}
          onClick={() => setUpdating(true)}
        >
          Update
        </Button>
        {updating ? (
          <UpdateDialog
            id={id}
            source={source}
            changeRef={false}
            onClose={() => setUpdating(false)}
          />
        ) : null}
      </TableCell>
    </TableRow>
  );
}

/** AttachedTable is every attached skill, by name, with its source. */
function AttachedTable({ rows, agents }: { rows: AttachedRow[]; agents: AgentPackage[] }) {
  return (
    <Table aria-label="Attached skills">
      <TableHeader>
        <TableRow>
          <TableHead>Skill</TableHead>
          <TableHead>Source</TableHead>
          <TableHead>Attached to</TableHead>
          <TableHead>Enabled</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((r) => (
          <TableRow key={`${r.id}/${r.name}`}>
            <TableCell data-label="">
              <div
                className={cn(
                  "flex min-w-0 flex-col gap-0.5",
                  !r.attachment.enabled && "opacity-70",
                )}
              >
                <Link
                  to="/skills/$id"
                  params={{ id: r.id }}
                  search={{ q: r.name }}
                  className={cn(
                    mono,
                    "w-fit font-semibold text-accent-text hover:text-accent-hover",
                  )}
                >
                  {r.name}
                </Link>
                <span className={cn("text-xs", r.missing ? "text-warn" : "text-fg-muted")}>
                  {r.missing ? "No longer in its source" : r.found?.description}
                </span>
              </div>
            </TableCell>
            <TableCell data-label="Source">
              <Link
                to="/skills/$id"
                params={{ id: r.id }}
                className={cn(mono, "font-medium text-fg-secondary hover:text-accent-text")}
              >
                {r.id}
              </Link>
            </TableCell>
            <TableCell data-label="Attached to" className="text-xs text-fg-secondary">
              {targetsText(r.attachment, agents)}
            </TableCell>
            <TableCell data-label="Enabled">
              <AttachedSwitch
                id={r.id}
                name={r.name}
                attachment={r.attachment}
                onApply={applyAndNotify}
              />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
