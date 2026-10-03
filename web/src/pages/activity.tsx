import { Link } from "@tanstack/react-router";
import { jobCommand, jobResultText, jobTitle } from "@/api/jobs";
import { useJob, useJobs } from "@/api/queries";
import { ApiError } from "@/api/request";
import { EmptyState } from "@/components/empty-state";
import { Facts, Mono } from "@/components/facts";
import { InlineAlert } from "@/components/inline-alert";
import { JobsTable } from "@/components/jobs-table";
import { LoadingRows } from "@/components/loading";
import { LogViewer } from "@/components/log-viewer";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
import { buttonVariants } from "@/components/ui/button";
import { duration, when } from "@/lib/format";
import { useTitle } from "@/lib/use-title";

const allJobs = (
  <Link to="/activity" className={buttonVariants({ variant: "outline" })}>
    All jobs
  </Link>
);

// Activity: job history and one job's live log.
export function ActivityPage() {
  useTitle("Activity");
  const jobs = useJobs();
  const list = jobs.data?.jobs ?? [];
  let body = <LoadingRows n={5} />;
  if (jobs.data) {
    body = (
      <PanelCard>
        {list.length ? (
          <JobsTable jobs={list} withDuration />
        ) : (
          <EmptyState text="No jobs yet. Updates, tool installs and sign-ins appear here." />
        )}
      </PanelCard>
    );
  }
  return (
    <Page>
      <PageHeader
        title="Activity"
        description="Jobs started from the panel since the container started."
      />
      {body}
    </Page>
  );
}

export function JobPage({ id }: { id: number }) {
  useTitle("Activity");
  const q = useJob(id);
  const job = q.data?.job;
  if (q.error instanceof ApiError && q.error.status === 404) {
    return (
      <Page>
        <InlineAlert tone="warn">That job is no longer in the history.</InlineAlert>
        <div>{allJobs}</div>
      </Page>
    );
  }
  if (!job) {
    return (
      <Page>
        <LoadingRows n={4} />
      </Page>
    );
  }
  const failure = jobResultText(job);
  return (
    <Page>
      <PageHeader title={jobTitle(job)} actions={allJobs} />
      <PanelCard
        title="Details"
        badge={<StatusBadge state={job.state === "running" ? "in_progress" : job.state} />}
      >
        <Facts
          rows={[
            ["Command", <Mono key="cmd">{jobCommand(job)}</Mono>],
            ["Started", when(job.started_at)],
            ["Finished", job.finished_at ? when(job.finished_at) : "—"],
            ["Duration", duration(job.started_at, job.finished_at)],
            ["Exit code", job.exit_code === null ? "—" : String(job.exit_code)],
          ]}
        />
        {failure ? <InlineAlert tone="bad">{failure}</InlineAlert> : null}
      </PanelCard>
      {job.device && job.state === "running" ? (
        <InlineAlert tone="info">
          Enter {job.device.code} at {job.device.url}.
        </InlineAlert>
      ) : null}
      <LogViewer text={job.log ?? ""} truncated={job.log_truncated} />
    </Page>
  );
}
