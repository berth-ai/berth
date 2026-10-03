import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { keys, queryClient, refreshStatus, useJobs, useStatus } from "@/api/queries";
import { verb } from "@/api/request";
import { runJob } from "@/api/run-job";
import type { ServiceStatus, Status, UpdateCheck } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { EmptyState } from "@/components/empty-state";
import { Facts, Mono } from "@/components/facts";
import { InlineAlert } from "@/components/inline-alert";
import { JobsTable } from "@/components/jobs-table";
import { LoadingRows } from "@/components/loading";
import { CardGrid, Page, PageHeader, SectionTitle } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
import { buttonVariants } from "@/components/ui/button";
import { errorText } from "@/lib/errors";
import { describeSource, SERVICE_LABELS, short, since, when } from "@/lib/format";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";

const SERVICES = ["panel", "ttyd", "code_server"];

// Overview: service health, the current agent set, update callout, recent jobs.
export function OverviewPage() {
  useTitle("Overview");
  const status = useStatus();
  const st = status.data;
  return (
    <Page>
      <PageHeader
        title="Overview"
        description="Health of your Berth and quick access to its frontends."
        actions={
          <>
            <Link to="/terminal" className={buttonVariants()}>
              Open Terminal
            </Link>
            <Link to="/ide" className={buttonVariants({ variant: "outline" })}>
              Open IDE
            </Link>
          </>
        }
      />
      {st?.check?.update_available ? <UpdateCallout check={st.check} /> : null}
      <SectionTitle>Services</SectionTitle>
      <CardGrid>
        {st ? (
          SERVICES.map((n) => (
            <ServiceCard key={n} name={n} svc={(st.services ?? []).find((s) => s.name === n)} />
          ))
        ) : (
          <LoadingRows n={3} />
        )}
      </CardGrid>
      <CardGrid>
        {st ? <AgentSetCard status={st} /> : null}
        <RecentJobs />
      </CardGrid>
    </Page>
  );
}

function ServiceCard({ name, svc }: { name: string; svc: ServiceStatus | undefined }) {
  const [busy, setBusy] = useState(false);
  const label = SERVICE_LABELS[name];
  if (!svc) {
    return (
      <PanelCard
        title={label}
        badge={<StatusBadge state="disabled" />}
        footer={
          <Link to="/settings" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Settings
          </Link>
        }
      >
        <p className="text-fg-muted">Switched off.</p>
      </PanelCard>
    );
  }
  const failed = svc.state === "failed";
  const troubled = failed || svc.state === "restarting";
  const retry = async () => {
    setBusy(true);
    try {
      await verb("service.restart", { name });
    } catch (err) {
      notify(errorText(err), "bad");
    }
    setBusy(false);
    void refreshStatus();
  };
  return (
    <PanelCard
      title={label}
      failed={failed}
      badge={
        <StatusBadge state={svc.state} detail={svc.restarts ? `${svc.restarts} restarts` : null} />
      }
      footer={
        troubled ? (
          <BusyButton size="sm" busy={busy} busyLabel="Restarting…" onClick={() => void retry()}>
            Retry
          </BusyButton>
        ) : null
      }
    >
      <Facts
        rows={[
          ["Uptime", svc.started_at ? since(svc.started_at) : "—"],
          ["Restarts", String(svc.restarts)],
        ]}
      />
      {troubled ? (
        <pre className="m-0 max-h-40 overflow-auto rounded-md bg-log-bg px-3 py-2.5 font-mono text-xs/[1.6] whitespace-pre text-log-fg">
          {(svc.log ?? []).slice(-8).join("\n") || "No output."}
        </pre>
      ) : null}
    </PanelCard>
  );
}

function AgentSetCard({ status }: { status: Status }) {
  const cur = status.agents?.current;
  return (
    <PanelCard
      title="Agent set"
      badge={
        status.agents?.follow_image ? <StatusBadge state="on" detail="follows the image" /> : null
      }
      footer={
        <Link to="/agents" className={buttonVariants({ variant: "outline", size: "sm" })}>
          Manage agents
        </Link>
      }
    >
      {cur ? (
        <Facts
          rows={[
            ["Lock", <Mono key="lock">{short(cur.hash)}</Mono>],
            ["Origin", describeSource(cur.source)],
            ["Installed", when(cur.installed_at)],
            ["Packages", String(Object.keys(cur.packages ?? {}).length)],
          ]}
        />
      ) : (
        <InlineAlert tone="warn">
          {status.agents_error ||
            "No agent set is installed yet. The first start installs one; watch Activity."}
        </InlineAlert>
      )}
    </PanelCard>
  );
}

function UpdateCallout({ check }: { check: UpdateCheck }) {
  const [busy, setBusy] = useState(false);
  const update = async () => {
    setBusy(true);
    const job = await runJob("update", {});
    setBusy(false);
    if (job)
      notify(
        job.state === "succeeded" ? "Agents updated." : "The update failed; see Activity.",
        job.state === "succeeded" ? "ok" : "bad",
      );
    void queryClient.invalidateQueries({ queryKey: keys.status });
  };
  return (
    <InlineAlert
      tone="warn"
      action={
        <BusyButton size="sm" busy={busy} busyLabel="Updating…" onClick={() => void update()}>
          Update
        </BusyButton>
      }
    >
      Agent update available on {check.ref}: {short(check.hash)}.
    </InlineAlert>
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
