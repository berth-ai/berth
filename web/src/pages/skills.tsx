import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useState } from "react";
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
import { useTitle } from "@/lib/use-title";
import { AvailablePicker } from "./available-picker";
import { type EditingSkill, SkillDialog } from "./skill-dialog";
import {
  agentsPatch,
  enabledPatch,
  reachLabel,
  removePatch,
  type SkillRow,
  shortCommit,
  skillAgents,
  skillRows,
  sourceLabel,
  unsupportedAgents,
  viaNotes,
} from "./skills-logic";

const BG = " Agents pick up the change in the background.";

// Skills: folders of instructions from git repositories, pinned and linked into each agent.
export function SkillsPage() {
  useTitle("Skills");
  const status = useStatus();
  const config = useConfig();
  const jobs = useJobs();
  const [dialog, setDialog] = useState<EditingSkill | "add" | null>(null);
  const [confirmDialog, confirm] = useConfirm();
  const packages = status.data?.packages ?? [];
  const agents = skillAgents(packages);
  const unsupported = unsupportedAgents(packages);
  const rows = config.data ? skillRows(config.data.config) : undefined;
  const lastSync = (jobs.data?.jobs ?? []).find((j) => j.verb === "skills.sync");

  const apply = async (patch: object, done: string) => {
    try {
      await applyConfig(patch);
      notify(done + BG);
    } catch (err) {
      notify(errorText(err), "bad");
    }
    await queryClient.invalidateQueries({ queryKey: keys.config });
  };

  return (
    <Page>
      <PageHeader
        title="Skills"
        description="Instructions and scripts your agents load on demand, pinned to a commit of a git repository."
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
            <span className="text-xs text-fg-muted">{count(rows.length, "skill", "skills")}</span>
          ) : null
        }
        actions={
          <Button size="sm" onClick={() => setDialog("add")}>
            <Plus aria-hidden /> Add skill
          </Button>
        }
      >
        {!rows ? (
          <LoadingRows n={2} />
        ) : rows.length ? (
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
              {rows.map((r) => (
                <SkillRowView
                  key={r.id}
                  row={r}
                  agents={agents}
                  unsupported={unsupported}
                  onApply={apply}
                  onEdit={() => setDialog(r)}
                  onRemove={() =>
                    confirm({
                      title: `Remove ${r.id}?`,
                      body: "Berth removes it from every agent and deletes its copy.",
                      confirm: "Remove",
                      danger: true,
                      action: () => apply(removePatch(r.id), `${r.id} removed.`),
                    })
                  }
                />
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyState text="No skills yet. Add one from the catalog or any git repository." />
        )}
      </PanelCard>
      {dialog ? (
        <SkillDialog
          agents={agents}
          unsupported={unsupported}
          installed={(rows ?? []).map((r) => r.id)}
          editing={dialog === "add" ? null : dialog}
          group={[]}
          onClose={() => setDialog(null)}
        />
      ) : null}
      {confirmDialog}
    </Page>
  );
}

function SkillRowView({
  row,
  agents,
  unsupported,
  onApply,
  onEdit,
  onRemove,
}: {
  row: SkillRow;
  agents: AgentPackage[];
  unsupported: AgentPackage[];
  onApply: (patch: object, done: string) => Promise<void>;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const { id, skill } = row;
  const [pending, setPending] = useState<boolean | null>(null);
  const [updating, setUpdating] = useState(false);

  const update = async () => {
    setUpdating(true);
    const job = await runJob("skills.update", { id });
    setUpdating(false);
    void queryClient.invalidateQueries({ queryKey: keys.config });
    if (!job) return;
    if (job.state !== "succeeded")
      notify(job.result?.error?.message ?? "The update failed; open Activity for its log.", "bad");
    else notify(job.result?.changed ? `${id} updated.${BG}` : "Already up to date.");
  };

  const toggle = async (on: boolean) => {
    setPending(on);
    await onApply(enabledPatch(id, on), `${id}${on ? " is on." : " is off."}`);
    setPending(null);
  };

  return (
    <TableRow>
      <TableCell data-label="">
        <div className="flex min-w-0 flex-col gap-0.5">
          <strong className="text-fg">{id}</strong>
          <span className="text-xs text-fg-muted">{skill.description}</span>
        </div>
      </TableCell>
      <TableCell data-label="Source">
        <div className="flex min-w-0 flex-col gap-0.5 font-mono text-xs" title={skill.repo}>
          <span className="break-all">{sourceLabel(skill)}</span>
          <span className="text-fg-muted">
            {skill.ref} · {shortCommit(skill.commit)}
          </span>
        </div>
      </TableCell>
      <TableCell data-label="Available to">
        <AvailablePicker
          server={id}
          value={skill.agents ?? undefined}
          agents={agents}
          unsupported={unsupported}
          notes={viaNotes(skill.agents, agents)}
          label={reachLabel(skill.agents, agents)}
          onChange={(next) =>
            onApply(
              agentsPatch(id, next),
              `${id} is available to ${next ? "the chosen agents" : "all agents"}.`,
            )
          }
        />
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
        <div className="inline-flex flex-wrap justify-end gap-2 sm:flex-nowrap">
          <BusyButton
            variant="outline"
            size="sm"
            busy={updating}
            busyLabel="Updating…"
            aria-label={`Update ${id}`}
            onClick={() => void update()}
          >
            Update
          </BusyButton>
          <Button variant="outline" size="sm" aria-label={`Edit ${id}`} onClick={onEdit}>
            Edit
          </Button>
          <Button variant="outline" size="sm" aria-label={`Remove ${id}`} onClick={onRemove}>
            Remove
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}
