import { Link } from "@tanstack/react-router";
import { jobTitle } from "@/api/jobs";
import type { Job } from "@/api/types";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { duration, when } from "@/lib/format";
import { StatusBadge } from "./status-badge";

export function JobsTable({ jobs, withDuration }: { jobs: Job[]; withDuration?: boolean }) {
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Job</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Started</TableHead>
            {withDuration ? <TableHead className="text-right">Duration</TableHead> : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {jobs.map((j) => (
            <TableRow key={j.id}>
              <TableCell data-label="Job">
                <Link
                  to="/activity/$id"
                  params={{ id: String(j.id) }}
                  className="font-semibold text-accent-text no-underline"
                >
                  {jobTitle(j)}
                </Link>
              </TableCell>
              <TableCell data-label="Status">
                <StatusBadge state={j.state === "running" ? "in_progress" : j.state} small />
              </TableCell>
              <TableCell data-label="Started">{when(j.started_at)}</TableCell>
              {withDuration ? (
                <TableCell data-label="Duration" className="text-right tabular-nums">
                  {duration(j.started_at, j.finished_at)}
                </TableCell>
              ) : null}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
