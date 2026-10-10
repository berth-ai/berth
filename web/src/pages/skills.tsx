import { Link, useNavigate } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useState } from "react";
import { useConfig, useJobs, useStatus } from "@/api/queries";
import type { AgentPackage, Source } from "@/api/types";
import { EmptyState } from "@/components/empty-state";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { Segmented } from "@/components/segmented";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { count } from "@/lib/format";
import { useTitle } from "@/lib/use-title";
import { cn } from "@/lib/utils";
import { applyAndNotify } from "./apply-notify";
import { AttachedSwitch } from "./attached-switch";
import { UpdateDialog } from "./source";
import { SourceDialog, type SourceStart } from "./source-dialog";
import { repoLabel, shortCommit, skillAgents, type SkillView } from "./skills-logic";
import {
  type AttachedRow,
  attachedRows,
  hasSuggested,
  missingNames,
  SUGGESTED,
  type SourceEntry,
  sourceList,
  sourceSummary,
  whoText,
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
    </Page>
  );
}

/** SourceTable lists the sources, then the Suggested row. */
function SourceTable({
  list,
  suggest,
  onSuggested,
}: {
  list: SourceEntry[];
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
          <SourceRow key={id} id={id} source={source} />
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

/** SourceRow is one source: repository and folder, ref and commit, counts. */
function SourceRow({ id, source }: { id: string; source: Source }) {
  const missing = missingNames(source).length;
  const [updating, setUpdating] = useState(false);
  return (
    <TableRow>
      <TableCell data-label="">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className={cn(mono, "font-semibold break-all text-fg")}>
            {repoLabel(source.repo)}
          </span>
          <span className={cn(mono, "text-fg-muted")}>
            {id} · {source.path || "/"}
          </span>
        </div>
      </TableCell>
      <TableCell data-label="Ref">
        <span className="flex flex-wrap gap-x-1.5 font-mono text-xs" title={source.commit}>
          <span className="break-all text-fg-secondary">{source.ref}</span>
          <span className="text-fg-muted">{shortCommit(source.commit)}</span>
        </span>
      </TableCell>
      <TableCell data-label="Skills" className="text-xs text-fg-secondary">
        {sourceSummary(source)}
        {missing ? <span className="text-warn"> · {missing} missing</span> : null}
      </TableCell>
      <TableCell className="text-right">
        <div className="inline-flex gap-2">
          <Button
            variant="outline"
            size="sm"
            aria-label={`Update ${id}`}
            onClick={() => setUpdating(true)}
          >
            Update
          </Button>
          <Link
            to="/skills/$id"
            params={{ id }}
            aria-label={`Manage ${id}`}
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            Manage
          </Link>
        </div>
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
                <span className={cn(mono, "font-semibold text-fg")}>{r.name}</span>
                <span className={cn("text-xs", r.missing ? "text-warn" : "text-fg-muted")}>
                  {r.missing ? "No longer in its source" : r.found?.description}
                </span>
              </div>
            </TableCell>
            <TableCell data-label="Source">
              <Link
                to="/skills/$id"
                params={{ id: r.id }}
                className={cn(mono, "font-medium text-accent-text")}
              >
                {r.id}
              </Link>
            </TableCell>
            <TableCell data-label="Attached to" className="text-xs text-fg-secondary">
              {whoText(r.attachment, agents)}
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
