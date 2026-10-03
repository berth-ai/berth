import type { Job } from "./types";

const TITLES: Record<string, string> = {
  update: "Agent update",
  rollback: "Agent rollback",
  gc: "Clean up agent versions",
  "mcp.setup": "MCP setup",
  "tools.add": "Add tool",
  "tools.remove": "Remove tool",
  "preset.install": "Install preset",
};

export function jobTitle(job: Job): string {
  if (job.verb === "forge.login") {
    return (job.args ?? []).some((a) => a.endsWith("glab"))
      ? "Sign in to GitLab"
      : "Sign in to GitHub";
  }
  return TITLES[job.verb] ?? job.verb;
}

/** jobCommand is the command a job runs, as the operator would type it. */
export function jobCommand(job: Job): string {
  const args = job.args ?? [];
  return job.group === "agents"
    ? `berth ${args.join(" ")}`
    : args.map((a) => a.replace(/^.*\//, "")).join(" ");
}

/** jobsInterval polls the job list every second while a job runs (spec §4.2). */
export function jobsInterval(jobs: Job[] | undefined): number {
  return (jobs ?? []).some((j) => j.state === "running") ? 1000 : 5000;
}

/** jobResultText explains a failed job's result, or returns null. */
export function jobResultText(job: Job): string | null {
  const e = job.result?.error;
  if (!e) return null;
  return (e.step ? `${e.step}: ` : "") + (e.message || e.code);
}
