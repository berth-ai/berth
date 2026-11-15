import type { ForgeStatus, Job } from "@/api/types";

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

const HOST = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]{1,5})?$/;

/** normalHost is a host as the broker stores it: trimmed, lower case, and
 * without a scheme or path, so a pasted https://gitlab.com/ still works. */
export function normalHost(s: string): string {
  return s
    .trim()
    .toLowerCase()
    .replace(/^[a-z]+:\/\//, "")
    .replace(/\/.*$/, "");
}

/** hostError names what is wrong with a host for a git identity, or "". */
export function hostError(s: string, taken: string[]): string {
  const host = normalHost(s);
  if (!host) return "";
  if (host.length > 253 || !HOST.test(host)) {
    return "Enter a host name such as gitlab.com, with an optional port.";
  }
  if (taken.includes(host)) return `${host} already has an identity; edit it instead.`;
  return "";
}

/** identityHosts are the code hosts Berth is signed in to that have no git
 * identity of their own yet, to offer first. */
export function identityHosts(data: ForgeStatus): string[] {
  const hosts: string[] = [];
  for (const id of ["gh", "glab"]) {
    const f = data.forges[id];
    if (f?.signed_in && f.host) hosts.push(f.host);
  }
  for (const l of data.forges.tea?.logins ?? []) {
    try {
      hosts.push(new URL(l.url).host);
    } catch {
      // A login without a usable URL offers nothing.
    }
  }
  const taken = new Set(data.identities.map((i) => i.host));
  return [...new Set(hosts.map(normalHost))].filter((h) => h && !taken.has(h));
}
