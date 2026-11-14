import { Link } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";
import { useState } from "react";
import {
  refreshAgentRegistry,
  refreshLogins,
  refreshStatus,
  refreshUsage,
  useAgentRegistry,
  useLogins,
  useStatus,
  useUsage,
} from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { AgentPackage, AgentUsage, Job, LoginStatus } from "@/api/types";
import { BrandIcon } from "@/components/brand-icon";
import { EmptyState } from "@/components/empty-state";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { CardGrid, Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { StatusIcon } from "@/components/status-badge";
import { UsageSpark } from "@/components/usage-bars";
import { Button, buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { errorText } from "@/lib/errors";
import { tokens } from "@/lib/format";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { AddAgentDialog } from "./agent-dialog";
import {
  addedToast,
  agentSlug,
  allAdded,
  cardStatus,
  signInSummary,
  usageLine,
} from "./agents-logic";

// Agents: one card per agent with its status and usage. Each agent's
// settings live on its own page; versions are on the Updates page.
export function AgentsPage() {
  useTitle("Agents");
  const status = useStatus();
  const agents = status.data
    ? (status.data.packages ?? []).filter((p) => p.kind === "agent")
    : undefined;
  const logins = useLogins();
  const usage = useUsage();
  const registry = useAgentRegistry();
  const [adding, setAdding] = useState(false);
  return (
    <Page>
      <PageHeader
        title="Agents"
        description="The agent CLIs Berth installs: who is signed in and how much each used. Open one to sign in, set keys or switch it off."
        actions={
          allAdded(registry.data?.agents) ? null : (
            <Button variant="outline" onClick={() => setAdding(true)}>
              Add agent
            </Button>
          )
        }
      />
      {usage.isError ? (
        <InlineAlert tone="warn">Usage is unavailable: {errorText(usage.error)}</InlineAlert>
      ) : null}
      {!agents ? (
        <PanelCard>
          <LoadingRows n={2} />
        </PanelCard>
      ) : agents.length ? (
        <CardGrid>
          {agents.map((p) => (
            <AgentCard
              key={p.package}
              p={p}
              slug={agentSlug(p.package, agents)}
              logins={logins.data}
              loginsLoading={logins.isPending}
              loginsFailed={logins.isError}
              usage={usage.data?.agents?.[p.package] ?? undefined}
              usageLoading={usage.isPending}
            />
          ))}
        </CardGrid>
      ) : (
        <PanelCard>
          <EmptyState text="No agents installed yet." />
        </PanelCard>
      )}
      {adding ? (
        <AddAgentDialog
          entries={registry.data?.agents}
          failed={registry.isError ? errorText(registry.error) : ""}
          onClose={() => setAdding(false)}
          onAdded={(name) => {
            notify(addedToast(name));
            void refreshStatus();
            void refreshLogins();
            void refreshUsage();
            void refreshAgentRegistry();
          }}
        />
      ) : null}
    </Page>
  );
}

/** AgentCard is an agent at a glance: one status icon, the version, and
 * its tokens today and this week. The details sit behind the icon and on
 * the agent's page. */
function AgentCard({
  p,
  slug,
  logins,
  loginsLoading,
  loginsFailed,
  usage,
  usageLoading,
}: {
  p: AgentPackage;
  slug: string;
  logins: LoginStatus | undefined;
  loginsLoading: boolean;
  loginsFailed: boolean;
  usage: AgentUsage | undefined;
  usageLoading: boolean;
}) {
  const st = cardStatus(
    p,
    signInSummary(p.enabled, logins?.agents?.[p.package]?.methods, loginsLoading),
    loginsFailed,
    usage?.error,
  );
  return (
    <PanelCard
      title={p.name}
      icon={<BrandIcon slug={p.icon} name={p.name} id={p.package} />}
      badge={
        st ? (
          <StatusIcon state={st.state} label={st.label}>
            {st.details.map((d) => (
              <p key={d} className="m-0 text-xs wrap-anywhere text-fg-secondary">
                {d}
              </p>
            ))}
          </StatusIcon>
        ) : (
          <Skeleton className="size-7" />
        )
      }
      actions={
        <Link
          to="/agents/$id"
          params={{ id: slug }}
          className={buttonVariants({ variant: "outline", size: "sm" })}
          aria-label={`Manage ${p.name}`}
        >
          Manage
          <ChevronRight aria-hidden />
        </Link>
      }
    >
      <span className="font-mono text-xs break-all text-fg-muted">
        {p.package} {p.version}
      </span>
      {usageLoading ? <LoadingRows n={2} /> : usage ? <CardUsage usage={usage} /> : null}
    </PanelCard>
  );
}

function CardUsage({ usage }: { usage: AgentUsage }) {
  if (!usage.tracked)
    return <p className="m-0 text-xs text-fg-muted">Usage isn't tracked for this agent.</p>;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-end justify-between gap-4">
        <dl className="m-0 flex gap-6">
          {(
            [
              ["Today", usage.today.total],
              ["Last 7 days", usage.week.total],
            ] as const
          ).map(([label, n]) => (
            <div key={label} className="flex flex-col">
              <dt className="text-xs text-fg-muted">{label}</dt>
              <dd className="m-0 text-md font-semibold tabular-nums">
                {tokens(n)} <span className="text-xs font-normal text-fg-muted">tokens</span>
              </dd>
            </div>
          ))}
        </dl>
        {usage.week.total ? (
          <UsageSpark days={usage.days ?? []} className="max-[360px]:hidden" />
        ) : null}
      </div>
      <p className="m-0 text-xs text-fg-muted tabular-nums">{usageLine(usage)}</p>
    </div>
  );
}

function finishJob(job: Job | null, okText: string) {
  if (!job) return;
  notify(
    job.state === "succeeded" ? okText : "The job failed; open Activity for its log.",
    job.state === "succeeded" ? "ok" : "bad",
  );
  void refreshStatus();
  if (job.verb === "agents.rollback") void refreshAgentRegistry();
}

/** startJob is a confirm action that starts a job and reports its outcome. */
export const startJob =
  (name: string, okText: string, body: unknown = {}) =>
  async () => {
    void runJob(name, body).then((job) => finishJob(job, okText));
    return true;
  };
