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

/** tokenPage is where a Gitea or Forgejo server issues access tokens, or
 * null until server is an https:// address. */
export function tokenPage(server: string): string | null {
  let u: URL;
  try {
    u = new URL(server.trim());
  } catch {
    return null;
  }
  if (u.protocol !== "https:" || !u.host) return null;
  return `${u.origin}${u.pathname.replace(/\/+$/, "")}/user/settings/applications`;
}
