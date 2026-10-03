import { Link } from "@tanstack/react-router";
import { jobTitle } from "@/api/jobs";
import { useJobs } from "@/api/queries";
import { Spinner } from "@/components/ui/spinner";

/** JobIndicator is visible from every page while a job runs (DESIGN.md §4). */
export function JobIndicator() {
  const jobs = useJobs();
  const running = (jobs.data?.jobs ?? []).filter((j) => j.state === "running");
  if (!running.length) return null;
  const first = running[0];
  return (
    <Link
      to="/activity/$id"
      params={{ id: String(first.id) }}
      className="flex flex-col gap-1.5 rounded-md border bg-info-soft p-2.5 text-xs text-fg-secondary no-underline"
    >
      <span className="flex items-center gap-1.5 text-sm font-semibold text-info">
        <Spinner className="size-3" />
        {running.length > 1 ? `${running.length} jobs running` : `${jobTitle(first)}…`}
      </span>
      <span aria-hidden className="h-1 overflow-hidden rounded-xs bg-line">
        <span className="block h-full w-2/5 animate-slide bg-info" />
      </span>
      <span>View log</span>
    </Link>
  );
}
