import { Link, useNavigate } from "@tanstack/react-router";
import { CircleCheck, CircleDashed } from "lucide-react";
import { useState } from "react";
import {
  refreshStatus,
  useConfig,
  useForges,
  useJobs,
  useLogins,
  useStatus,
  useUsage,
  useWorkspaceStatus,
} from "@/api/queries";
import { verb } from "@/api/request";
import type { AgentPackage, ServiceStatus, Status } from "@/api/types";
import { launchIn } from "@/api/workspaces";
import { BusyButton } from "@/components/busy-button";
import { EmptyState } from "@/components/empty-state";
import { InlineAlert } from "@/components/inline-alert";
import { JobsTable } from "@/components/jobs-table";
import { LaunchMenu } from "@/components/launch-menu";
import { LoadingRows } from "@/components/loading";
import { LogText } from "@/components/log-viewer";
import { CardGrid, Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
import { buttonVariants } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorText } from "@/lib/errors";
import { SERVICE_LABELS, since, tokens } from "@/lib/format";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { agentSlug, cardStatus, signInSummary } from "./agents-logic";
import { healthRows, healthSummary, setupSteps, troubled } from "./overview-logic";
import { SessionBadge } from "./workspaces";
import { gitSummary, launchAgents, workspaceRows } from "./workspaces-logic";

/** WORKSPACES is how many workspaces the Overview lists before "All". */
const WORKSPACES = 6;

// Overview: what the user works with first (setup checklist, workspaces
// with Launch, agents and their sign-in), then health and recent jobs.
export function OverviewPage() {
  useTitle("Overview");
  const status = useStatus();
  const st = status.data;
  const ide = st?.services?.some((s) => s.name === "code_server");
  return (
    <Page>
      <PageHeader
        title="Overview"
        description="Your workspaces and agents, and whether Berth is healthy."
        actions={
          <>
            <Link to="/terminal" className={buttonVariants()}>
              Open Terminal
            </Link>
            {ide ? (
              <Link to="/ide" className={buttonVariants({ variant: "outline" })}>
                Open IDE
              </Link>
            ) : null}
          </>
        }
      />
      <GettingStarted status={st} />
      {st ? <TroubledServices status={st} /> : null}
      <WorkspacesCard status={st} />
      <CardGrid>
        <AgentsCard status={st} />
        {st ? <HealthCard status={st} /> : <LoadingRows n={3} />}
      </CardGrid>
      <RecentJobs />
    </Page>
  );
}

/** GettingStarted is the checklist a new Berth keeps on the Overview
 * until an agent can run, git knows who commits, and a workspace exists. */
function GettingStarted({ status }: { status: Status | undefined }) {
  const logins = useLogins();
  const forges = useForges();
  const config = useConfig();
  const steps = setupSteps({
    packages: status?.packages ?? undefined,
    logins: logins.data,
    git: forges.data?.git,
    workspaces: config.data ? workspaceRows(config.data.config).length : undefined,
  });
  if (!steps || steps.every((s) => s.done)) return null;
  const done = steps.filter((s) => s.done).length;
  return (
    <PanelCard
      title="Get started"
      badge={
        <span className="text-xs text-fg-muted tabular-nums">
          {done} of {steps.length} done
        </span>
      }
    >
      <ol aria-label="Get started" className="m-0 flex list-none flex-col gap-2.5 p-0">
        {steps.map((s) => (
          <li key={s.id} className="flex flex-wrap items-center gap-x-3 gap-y-1">
            {s.done ? (
              <CircleCheck aria-hidden className="size-4 text-ok" strokeWidth={2} />
            ) : (
              <CircleDashed aria-hidden className="size-4 text-fg-muted" strokeWidth={2} />
            )}
            <div className="flex min-w-0 flex-[1_1_240px] flex-col">
              <span className={s.done ? "text-fg-muted line-through" : "font-medium"}>
                {s.label}
                <span className="sr-only">{s.done ? " (done)" : " (to do)"}</span>
              </span>
              {s.done ? null : <span className="text-xs text-fg-muted">{s.hint}</span>}
            </div>
            {s.done ? null : (
              <Link to={s.to} className={buttonVariants({ variant: "outline", size: "sm" })}>
                {s.action}
              </Link>
            )}
          </li>
        ))}
      </ol>
    </PanelCard>
  );
}

function WorkspacesCard({ status }: { status: Status | undefined }) {
  const config = useConfig();
  const states = useWorkspaceStatus();
  const navigate = useNavigate();
  const rows = config.data ? workspaceRows(config.data.config) : null;
  const agents = launchAgents(status?.packages ?? []);
  const more = rows ? rows.length - WORKSPACES : 0;
  return (
    <PanelCard
      title="Workspaces"
      footer={
        <Link to="/workspaces" className={buttonVariants({ variant: "outline", size: "sm" })}>
          {more > 0 ? `All workspaces (${rows?.length})` : "All workspaces"}
        </Link>
      }
    >
      {!rows ? (
        <LoadingRows n={2} />
      ) : rows.length ? (
        <Table aria-label="Workspaces">
          <TableHeader>
            <TableRow>
              <TableHead>Workspace</TableHead>
              <TableHead>Git</TableHead>
              <TableHead>Session</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.slice(0, WORKSPACES).map(({ id }) => {
              const st = states.data?.workspaces[id];
              return (
                <TableRow key={id}>
                  <TableCell>
                    <Link
                      to="/workspaces/$id"
                      params={{ id }}
                      className="font-semibold text-accent-text no-underline"
                    >
                      {id}
                    </Link>
                  </TableCell>
                  <TableCell data-label="Git" className="text-fg-secondary">
                    {gitSummary(st)}
                  </TableCell>
                  <TableCell data-label="Session">
                    <SessionBadge st={st} />
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end">
                      <LaunchMenu
                        label={`Launch in ${id}`}
                        agents={agents}
                        disabled={st?.exists === false}
                        onLaunch={(agent) =>
                          void launchIn(id, agent).then((ok) => {
                            if (ok)
                              void navigate({
                                to: "/workspaces/$id",
                                params: { id },
                                search: { tab: "terminal" },
                              });
                          })
                        }
                      />
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          text="No workspaces yet. Add one to launch agents in it."
          action={
            <Link to="/workspaces" className={buttonVariants({ variant: "outline" })}>
              Add workspace
            </Link>
          }
        />
      )}
    </PanelCard>
  );
}

/** AgentsCard lists the agents with their sign-in in words and today's
 * tokens; brand marks stay on the Agents page (DESIGN.md §4). */
function AgentsCard({ status }: { status: Status | undefined }) {
  const logins = useLogins();
  const usage = useUsage();
  const all = status?.packages ?? [];
  const agents = all.filter((p) => p.kind === "agent");
  return (
    <PanelCard
      title="Agents"
      footer={
        <Link to="/agents" className={buttonVariants({ variant: "outline", size: "sm" })}>
          Manage agents
        </Link>
      }
    >
      {!status ? (
        <LoadingRows n={3} />
      ) : !status.agents?.current ? (
        <InlineAlert tone="warn">
          {status.agents_error ||
            "The agents are not installed yet. The first start installs them; this takes a few minutes."}{" "}
          <Link to="/activity" className="underline">
            Watch it in Activity
          </Link>
        </InlineAlert>
      ) : agents.length ? (
        <ul aria-label="Agents" className="m-0 flex list-none flex-col gap-2 p-0">
          {agents.map((p) => (
            <AgentRow
              key={p.package}
              p={p}
              slug={agentSlug(p.package, agents)}
              status={cardStatus(
                p,
                signInSummary(
                  p.enabled,
                  logins.data?.agents?.[p.package]?.methods,
                  logins.isPending,
                ),
                logins.isError,
                usage.data?.agents?.[p.package]?.error,
              )}
              today={usage.data?.agents?.[p.package]}
            />
          ))}
        </ul>
      ) : (
        <EmptyState text="No agents are installed." />
      )}
    </PanelCard>
  );
}

function AgentRow({
  p,
  slug,
  status,
  today,
}: {
  p: AgentPackage;
  slug: string;
  status: ReturnType<typeof cardStatus>;
  today: { tracked: boolean; today: { total: number } } | undefined;
}) {
  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <Link
        to="/agents/$id"
        params={{ id: slug }}
        className="min-w-0 flex-[1_1_120px] font-semibold text-accent-text no-underline"
      >
        {p.name}
      </Link>
      {status ? (
        <span className="flex min-w-0 items-center gap-2">
          <StatusBadge state={status.state} small />
          {status.state === "signed_in" && status.label !== "Signed in" ? (
            <span className="truncate text-xs text-fg-muted">
              {status.label.replace(/^Signed in as /, "")}
            </span>
          ) : null}
        </span>
      ) : null}
      {p.enabled && today?.tracked ? (
        <span className="text-xs text-fg-muted tabular-nums">
          {tokens(today.today.total)} tokens today
        </span>
      ) : null}
    </li>
  );
}

/** TroubledServices gives a failed or restarting service a full card with
 * its log and Retry, above everything else. */
function TroubledServices({ status }: { status: Status }) {
  const bad = (status.services ?? []).filter(troubled);
  if (!bad.length) return null;
  return (
    <CardGrid>
      {bad.map((s) => (
        <ServiceCard key={s.name} svc={s} />
      ))}
    </CardGrid>
  );
}

const label = (name: string) => SERVICE_LABELS[name] ?? name;

/** HealthCard is one line while all is well, then each service with its
 * badge and uptime; the panel only after a recent restart. */
function HealthCard({ status }: { status: Status }) {
  const services = status.services ?? [];
  const rows = healthRows(services);
  const sum = healthSummary(services);
  return (
    <PanelCard title="Health" badge={<StatusBadge state={sum.state} />}>
      <p className="text-fg-secondary">{sum.text}</p>
      <ul aria-label="Services" className="m-0 flex list-none flex-col gap-1.5 p-0">
        {rows.map((s) => (
          <li key={s.name} className="flex flex-wrap items-center gap-2">
            <span className="min-w-20 font-medium">{label(s.name)}</span>
            <StatusBadge
              state={s.state}
              detail={s.restarts ? `${s.restarts} restarts` : null}
              small
            />
            {s.started_at && s.state === "running" ? (
              <span className="text-xs text-fg-muted tabular-nums">up {since(s.started_at)}</span>
            ) : null}
          </li>
        ))}
      </ul>
    </PanelCard>
  );
}

function ServiceCard({ svc }: { svc: ServiceStatus }) {
  const [busy, setBusy] = useState(false);
  const failed = svc.state === "failed";
  const retry = async () => {
    setBusy(true);
    try {
      await verb("service.restart", { name: svc.name });
    } catch (err) {
      notify(errorText(err), "bad");
    }
    setBusy(false);
    void refreshStatus();
  };
  return (
    <PanelCard
      title={label(svc.name)}
      failed={failed}
      badge={
        <StatusBadge state={svc.state} detail={svc.restarts ? `${svc.restarts} restarts` : null} />
      }
      footer={
        <BusyButton size="sm" busy={busy} busyLabel="Restarting…" onClick={() => void retry()}>
          Retry
        </BusyButton>
      }
    >
      <LogText
        className="max-h-40 rounded-md"
        text={(svc.log ?? []).slice(-8).join("\n") || "No output."}
      />
    </PanelCard>
  );
}

function RecentJobs() {
  const jobs = useJobs();
  const list = jobs.data?.jobs ?? [];
  return (
    <PanelCard
      title="Recent jobs"
      footer={
        <Link to="/activity" className={buttonVariants({ variant: "outline", size: "sm" })}>
          All activity
        </Link>
      }
    >
      {list.length ? <JobsTable jobs={list.slice(0, 5)} /> : <EmptyState text="No jobs yet." />}
    </PanelCard>
  );
}
