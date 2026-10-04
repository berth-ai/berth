import { Link } from "@tanstack/react-router";
import { ChevronDown, Plus, TriangleAlert } from "lucide-react";
import { Fragment, useState } from "react";
import { applyConfig } from "@/api/config";
import { keys, queryClient, useConfig, useJobs, useStatus } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { AgentPackage } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { OnOffSwitch } from "@/components/on-off-switch";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { Segmented } from "@/components/segmented";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
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
import { AgentChecklist } from "./available-picker";
import { RefDialog } from "./ref-dialog";
import { SkillDialog, type SkillSource } from "./skill-dialog";
import {
  agentsPatch,
  commonDir,
  drifted,
  enabledPatch,
  enabledState,
  flatRows,
  groupAgentsPatch,
  groupEnabledPatch,
  groupOf,
  groupReach,
  groupRemovePatch,
  removePatch,
  repoLabel,
  shortCommit,
  type SkillGroup,
  type SkillRow,
  type SkillView,
  skillAgents,
  skillGroups,
  skillLocation,
  sourceLabel,
  unsupportedAgents,
  updateText,
  usedBy,
  viaNotes,
} from "./skills-logic";
import { UpdateChoice } from "./update-choice";

const BG = " Agents pick up the change in the background.";
const VIEWS = [
  { value: "source", label: "By source" },
  { value: "flat", label: "All skills" },
] as const;

type Apply = (patch: object, done: string) => Promise<void>;
type RefTarget = { row: SkillRow; group: SkillRow[]; whole?: boolean; defaultAll?: boolean };

// toggled adds key to set, or takes it out.
const toggled = (set: Set<string>, key: string) => {
  const next = new Set(set);
  if (!next.delete(key)) next.add(key);
  return next;
};

// Skills: folders of instructions from git repositories, pinned and linked into each agent.
export function SkillsPage({ view, onView }: { view: SkillView; onView: (v: SkillView) => void }) {
  useTitle("Skills");
  const status = useStatus();
  const config = useConfig();
  const jobs = useJobs();
  const [adding, setAdding] = useState<SkillSource | true | null>(null);
  const [refTarget, setRefTarget] = useState<RefTarget | null>(null);
  const [confirmDialog, confirm] = useConfirm();
  const [choice, setChoice] = useState<{ id: string; ids: string[] } | null>(null);
  const [updating, setUpdating] = useState<string[]>([]);
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const packages = status.data?.packages ?? [];
  const agents = skillAgents(packages);
  const unsupported = unsupportedAgents(packages);
  const groups = config.data ? skillGroups(config.data.config) : undefined;
  const rows = groups?.flatMap((g) => g.rows);
  const lastSync = (jobs.data?.jobs ?? []).find((j) => j.verb === "skills.sync");

  const runUpdate = async (ids: string[]) => {
    setUpdating(ids);
    const job = await runJob("skills.update", { ids });
    setUpdating([]);
    void queryClient.invalidateQueries({ queryKey: keys.config });
    if (!job) return;
    if (job.state !== "succeeded")
      notify(job.result?.error?.message ?? "The update failed; open Activity for its log.", "bad");
    else notify(updateText(job, BG));
  };

  const apply: Apply = async (patch, done) => {
    try {
      await applyConfig(patch);
      notify(done + BG);
    } catch (err) {
      notify(errorText(err), "bad");
    }
    await queryClient.invalidateQueries({ queryKey: keys.config });
  };

  const removeOne = (r: SkillRow) =>
    confirm({
      title: `Remove ${r.id}?`,
      body: "Berth removes it from every agent and deletes its copy.",
      confirm: "Remove",
      danger: true,
      action: () => apply(removePatch(r.id), `${r.id} removed.`),
    });

  const rowProps = (r: SkillRow, group: SkillRow[], inGroup: boolean) => ({
    row: r,
    inGroup,
    agents,
    unsupported,
    open: open.has(r.id),
    updating: updating.includes(r.id),
    onToggle: () => setOpen((s) => toggled(s, r.id)),
    onApply: apply,
    // In the flat view a grouped skill asks whether its siblings move too.
    onUpdate: () =>
      !inGroup && group.length > 1
        ? setChoice({ id: r.id, ids: group.map((g) => g.id) })
        : void runUpdate([r.id]),
    onRef: () => setRefTarget({ row: r, group, defaultAll: !inGroup }),
    onRemove: () => removeOne(r),
  });

  let body;
  if (!groups || !rows) body = <LoadingRows n={2} />;
  else if (!rows.length)
    body = <EmptyState text="No skills yet. Add one from the catalog or any git repository." />;
  else
    body = (
      <Table aria-label="Skills">
        <TableHeader>
          <TableRow>
            <TableHead>Skill</TableHead>
            <TableHead>Source</TableHead>
            <TableHead>Available to</TableHead>
            <TableHead>Enabled</TableHead>
            <TableHead>
              <span className="sr-only">Actions</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {view === "flat"
            ? flatRows(groups).map((r) => (
                <SkillRowView key={r.id} {...rowProps(r, groupOf(groups, r.id), false)} />
              ))
            : groups.map((g) =>
                g.rows.length > 1 ? (
                  <Fragment key={g.key}>
                    <GroupRows
                      group={g}
                      agents={agents}
                      unsupported={unsupported}
                      open={open.has(g.key)}
                      collapsed={collapsed.has(g.key)}
                      updating={g.rows.every((r) => updating.includes(r.id))}
                      onToggle={() => setOpen((s) => toggled(s, g.key))}
                      onCollapse={() => setCollapsed((s) => toggled(s, g.key))}
                      onApply={apply}
                      onUpdate={() => void runUpdate(g.rows.map((r) => r.id))}
                      onRef={() => setRefTarget({ row: g.rows[0], group: g.rows, whole: true })}
                      onAddMore={() =>
                        setAdding({
                          repo: g.repo,
                          ref: g.ref,
                          path: commonDir(g.rows.map((r) => r.skill.path)),
                        })
                      }
                      onRemove={() =>
                        confirm({
                          title: `Remove ${count(g.rows.length, "skill", "skills")} from ${repoLabel(g.repo)}?`,
                          body: `Berth removes ${g.rows.map((r) => r.id).join(", ")} from every agent and deletes their copies.`,
                          confirm: `Remove ${g.rows.length} skills`,
                          danger: true,
                          action: () =>
                            apply(
                              groupRemovePatch(g.rows),
                              `${count(g.rows.length, "skill", "skills")} removed.`,
                            ),
                        })
                      }
                    />
                    {collapsed.has(g.key)
                      ? null
                      : g.rows.map((r) => (
                          <SkillRowView key={r.id} {...rowProps(r, g.rows, true)} />
                        ))}
                  </Fragment>
                ) : (
                  <SkillRowView key={g.key} {...rowProps(g.rows[0], g.rows, false)} />
                ),
              )}
        </TableBody>
      </Table>
    );

  return (
    <Page>
      <PageHeader
        title="Skills"
        description="Folders with a SKILL.md that agents load when a task calls for them. Berth keeps one copy of each and links it into the skills folder of the agents you choose."
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
        title="Installed"
        badge={
          rows ? (
            <span className="text-xs text-fg-muted">
              {count(rows.length, "skill", "skills")} · {rows.filter((r) => r.skill.enabled).length}{" "}
              enabled
            </span>
          ) : null
        }
        actions={
          <div className="flex flex-wrap items-center gap-2.5">
            <Segmented label="View" value={view} options={VIEWS} onChange={onView} size="sm" />
            <Button size="sm" onClick={() => setAdding(true)}>
              <Plus aria-hidden /> Add skill
            </Button>
          </div>
        }
      >
        {body}
      </PanelCard>
      {adding ? (
        <SkillDialog
          agents={agents}
          unsupported={unsupported}
          installed={(rows ?? []).map((r) => r.id)}
          source={adding === true ? undefined : adding}
          onClose={() => setAdding(null)}
        />
      ) : null}
      {refTarget ? <RefDialog {...refTarget} onClose={() => setRefTarget(null)} /> : null}
      {confirmDialog}
      {choice ? (
        <UpdateChoice
          id={choice.id}
          ids={choice.ids}
          onClose={() => setChoice(null)}
          onPick={(ids) => {
            setChoice(null);
            void runUpdate(ids);
          }}
        />
      ) : null}
    </Page>
  );
}

/** ManageButton opens a row's panel; its chevron turns when open. */
function ManageButton({
  open,
  label,
  onClick,
}: {
  open: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <Button variant="outline" size="sm" aria-expanded={open} aria-label={label} onClick={onClick}>
      Manage
      <ChevronDown
        aria-hidden
        data-icon="inline-end"
        className={cn("transition-transform", open && "rotate-180")}
      />
    </Button>
  );
}

/** reachClass marks a skill no agent can load. */
const reachClass = (label: string) =>
  label === "No installed agent" ? "text-warn" : "text-fg-secondary";

/** GroupRows is the header row of skills sharing a repository and ref, and
 * its Manage panel (DESIGN.md §4). */
function GroupRows({
  group,
  agents,
  unsupported,
  open,
  collapsed,
  updating,
  onToggle,
  onCollapse,
  onApply,
  onUpdate,
  onRef,
  onAddMore,
  onRemove,
}: {
  group: SkillGroup;
  agents: AgentPackage[];
  unsupported: AgentPackage[];
  open: boolean;
  collapsed: boolean;
  updating: boolean;
  onToggle: () => void;
  onCollapse: () => void;
  onApply: Apply;
  onUpdate: () => void;
  onRef: () => void;
  onAddMore: () => void;
  onRemove: () => void;
}) {
  const { rows } = group;
  const src = repoLabel(group.repo);
  const n = rows.length;
  const commits = drifted(rows);
  const state = enabledState(rows);
  const used = usedBy(rows, agents);
  const [pending, setPending] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);
  const toggle = async (on: boolean) => {
    setPending(on);
    await onApply(
      groupEnabledPatch(rows, on),
      `${count(n, "skill", "skills")} from ${src} ${on ? "are on." : "are off."}`,
    );
    setPending(null);
  };
  const give = async (a: AgentPackage, on: boolean) => {
    setSaving(true);
    await onApply(
      groupAgentsPatch(
        rows,
        agents.map((x) => x.package),
        a.package,
        on,
      ),
      `${count(n, "skill", "skills")} from ${src} ${on ? "are available to" : "are no longer available to"} ${a.name}.`,
    );
    setSaving(false);
  };
  const update = (
    <BusyButton
      variant="outline"
      size="sm"
      busy={updating}
      busyLabel="Updating…"
      aria-label={`${commits ? "Realign" : "Update all"} skills from ${src}`}
      onClick={onUpdate}
    >
      {commits ? "Realign" : "Update all"}
    </BusyButton>
  );

  return (
    <>
      <TableRow className="bg-surface-sunken hover:bg-surface-sunken">
        <TableCell data-label="" className="first:pl-2">
          <div className="flex min-w-0 items-center gap-1.5">
            <Button
              variant="ghost"
              size="icon-xs"
              className="text-fg-secondary hover:bg-line hover:text-fg"
              aria-expanded={!collapsed}
              aria-label={`${collapsed ? "Expand" : "Collapse"} ${src}`}
              onClick={onCollapse}
            >
              <ChevronDown
                aria-hidden
                className={cn("transition-transform", collapsed && "-rotate-90")}
              />
            </Button>
            <span className="flex min-w-0 flex-col">
              <span className="font-mono text-xs font-semibold break-all text-fg">{src}</span>
              <span className="text-xs text-fg-muted">{count(n, "skill", "skills")}</span>
            </span>
          </div>
        </TableCell>
        <TableCell data-label="Source">
          <div className="flex min-w-0 flex-col gap-0.5 text-xs">
            <span className="flex flex-wrap gap-x-1.5 font-mono text-fg-secondary">
              <span className="break-all">{group.ref}</span>
              {commits ? null : (
                <span className="text-fg-muted">{shortCommit(rows[0].skill.commit)}</span>
              )}
            </span>
            {commits ? (
              <span className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 font-medium text-warn">
                <span className="inline-flex items-center gap-1">
                  <TriangleAlert aria-hidden className="size-3" strokeWidth={2} />
                  On {commits} commits
                </span>
                <Button
                  variant="link"
                  size="xs"
                  className="h-auto px-0"
                  disabled={updating}
                  onClick={onUpdate}
                >
                  Realign
                </Button>
              </span>
            ) : null}
          </div>
        </TableCell>
        <TableCell data-label="Available to" className={cn("text-xs", reachClass(used))}>
          {used}
        </TableCell>
        <TableCell data-label="Enabled">
          <OnOffSwitch
            checked={pending ?? state.on}
            word={pending === null ? state.word : undefined}
            disabled={pending !== null}
            label={`${state.on ? "Turn off" : "Turn on"} all skills from ${src}`}
            onCheckedChange={(on) => void toggle(on)}
          />
        </TableCell>
        <TableCell className="text-right">
          <ManageButton open={open} label={`Manage skills from ${src}`} onClick={onToggle} />
        </TableCell>
      </TableRow>
      {open && !collapsed ? (
        <TableRow className="bg-surface-sunken hover:bg-surface-sunken">
          <TableCell colSpan={5} className="pt-1 pb-4 first:pl-10">
            <div className="flex flex-col gap-3.5">
              <fieldset className="flex flex-col gap-1.5" disabled={saving}>
                <legend className="mb-1.5 font-medium text-fg">Available to, for all {n}</legend>
                {groupReach(rows, agents).map(({ agent, n: has }) => (
                  <Label
                    key={agent.package}
                    className="flex flex-wrap items-center gap-2 leading-[inherit] font-normal"
                  >
                    <Checkbox
                      checked={has === n}
                      disabled={saving}
                      onCheckedChange={(on) => void give(agent, on)}
                    />
                    {agent.name}
                    {agent.skills_dir ? (
                      <span className="font-mono text-xs text-fg-muted">{agent.skills_dir}</span>
                    ) : null}
                    {has > 0 && has < n ? (
                      <span className="text-xs text-warn">
                        {has} of {n}
                      </span>
                    ) : null}
                  </Label>
                ))}
                {unsupported.map((a) => (
                  <Label
                    key={a.package}
                    className="flex items-center gap-2 leading-[inherit] font-normal text-fg-muted"
                  >
                    <Checkbox checked={false} disabled />
                    {a.name}
                    <span className="text-xs">Doesn't support skills</span>
                  </Label>
                ))}
              </fieldset>
              <div className="flex flex-wrap items-center gap-2">
                <span className="min-w-50 flex-1 text-xs text-fg-muted">
                  From <span className="font-mono">{src}</span> at{" "}
                  <span className="font-mono">{group.ref}</span>.
                </span>
                {update}
                <Button variant="outline" size="sm" onClick={onRef}>
                  Change ref
                </Button>
                <Button variant="outline" size="sm" onClick={onAddMore}>
                  Add more from this source
                </Button>
                <Button variant="outline" size="sm" onClick={onRemove}>
                  Remove all
                </Button>
              </div>
            </div>
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}

/** SkillRowView is one skill and its Manage panel. inGroup indents it under
 * its group's header row, which then carries the repository and ref. */
function SkillRowView({
  row,
  inGroup,
  agents,
  unsupported,
  open,
  updating,
  onToggle,
  onApply,
  onUpdate,
  onRef,
  onRemove,
}: {
  row: SkillRow;
  inGroup: boolean;
  agents: AgentPackage[];
  unsupported: AgentPackage[];
  open: boolean;
  updating: boolean;
  onToggle: () => void;
  onApply: Apply;
  onUpdate: () => void;
  onRef: () => void;
  onRemove: () => void;
}) {
  const { id, skill } = row;
  const [pending, setPending] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);
  const used = usedBy([row], agents);
  const toggle = async (on: boolean) => {
    setPending(on);
    await onApply(enabledPatch(id, on), `${id}${on ? " is on." : " is off."}`);
    setPending(null);
  };
  const pick = async (next: string[] | null) => {
    setSaving(true);
    await onApply(
      agentsPatch(id, next),
      `${id} is available to ${next ? "the chosen agents" : "all agents"}.`,
    );
    setSaving(false);
  };

  return (
    <>
      <TableRow>
        <TableCell data-label="" className={cn(inGroup && "first:pl-10")}>
          <div className={cn("flex min-w-0 flex-col gap-0.5", !skill.enabled && "opacity-70")}>
            <span className="font-mono text-xs font-semibold text-fg">{id}</span>
            <span className="text-xs text-fg-muted">{skill.description}</span>
          </div>
        </TableCell>
        <TableCell data-label="Source">
          <div className="flex min-w-0 flex-col font-mono text-xs" title={skill.commit}>
            {inGroup ? null : (
              <span className="break-all text-fg-secondary">{sourceLabel(skill)}</span>
            )}
            <span className="text-fg-muted">
              {inGroup ? null : `${skill.ref} · `}
              {shortCommit(skill.commit)}
            </span>
          </div>
        </TableCell>
        <TableCell data-label="Available to" className={cn("text-xs", reachClass(used))}>
          {used}
        </TableCell>
        <TableCell data-label="Enabled">
          <OnOffSwitch
            checked={pending ?? skill.enabled}
            disabled={pending !== null}
            label={`${skill.enabled ? "Turn off" : "Turn on"} ${id}`}
            onCheckedChange={(on) => void toggle(on)}
          />
        </TableCell>
        <TableCell className="text-right">
          <ManageButton open={open} label={`Manage ${id}`} onClick={onToggle} />
        </TableCell>
      </TableRow>
      {open ? (
        <TableRow className="hover:bg-transparent">
          <TableCell colSpan={5} className={cn("pt-1 pb-4", inGroup && "first:pl-10")}>
            <div className="flex flex-col gap-3.5">
              <div className="flex flex-col gap-1.5">
                <span className="font-medium text-fg">Location</span>
                <code className="max-w-full self-start overflow-x-auto whitespace-nowrap">
                  {skillLocation(id)}
                </code>
              </div>
              <AgentChecklist
                value={skill.agents}
                agents={agents}
                unsupported={unsupported}
                notes={viaNotes(skill.agents, agents)}
                disabled={saving}
                onChange={(next) => void pick(next)}
              />
              <div className="flex flex-wrap items-center gap-2">
                <span className="min-w-50 flex-1 text-xs text-fg-muted">
                  From <span className="font-mono">{sourceLabel(skill)}</span> at{" "}
                  <span className="font-mono">{skill.ref}</span>, pinned to{" "}
                  <span className="font-mono">{shortCommit(skill.commit)}</span>.
                </span>
                <BusyButton
                  variant="outline"
                  size="sm"
                  busy={updating}
                  busyLabel="Updating…"
                  aria-label={`Update ${id}`}
                  onClick={onUpdate}
                >
                  Update
                </BusyButton>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Change ref for ${id}`}
                  onClick={onRef}
                >
                  Change ref
                </Button>
                <Button variant="outline" size="sm" aria-label={`Remove ${id}`} onClick={onRemove}>
                  Remove skill
                </Button>
              </div>
            </div>
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}
