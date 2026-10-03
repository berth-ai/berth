import type { Job } from "@/api/types";

export function deviceHost(url: string): string {
  return url.replace(/^https:\/\//, "").replace(/\?.*$/, "");
}

/** forgeJobId is the running forge.login job for forge, or null. */
export function forgeJobId(jobs: Job[] | undefined, forge: string): number | null {
  const job = (jobs ?? []).find(
    (j) =>
      j.verb === "forge.login" &&
      j.state === "running" &&
      (j.args ?? []).some((a) => a.endsWith(forge)),
  );
  return job ? job.id : null;
}
