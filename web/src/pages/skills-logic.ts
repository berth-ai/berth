import type { AgentPackage } from "@/api/types";

/** normalizeRepo turns owner/repo and host/owner/repo into an https URL. */
export function normalizeRepo(input: string): string {
  const s = input.trim();
  if (/^[\w.-]+\/[\w.-]+$/.test(s)) return `https://github.com/${s}`;
  if (/^[\w.-]+\.[a-z]{2,}\/\S+$/i.test(s)) return `https://${s}`;
  return s;
}

/** repoError mirrors the broker's CheckSkillRepo after normalizing. */
export function repoError(raw: string): string {
  const s = normalizeRepo(raw);
  if (!s) return "Enter the repository, such as anthropics/skills.";
  if (s.length > 2048) return "Use at most 2048 characters.";
  let u: URL;
  try {
    u = new URL(s);
  } catch {
    return "Enter a repository URL such as https://github.com/owner/repo.";
  }
  if (u.protocol !== "https:") return "Start the URL with https://.";
  if (u.username || u.password)
    return "Leave the user name and password out; sign in on the Accounts page instead.";
  if (u.search || u.hash || s.includes("?") || s.includes("#"))
    return "Leave ? and # out of the URL.";
  return "";
}

/** pathError mirrors ValidSkillPath. */
export function pathError(p: string): string {
  if (!p) return "";
  const bad =
    p.length > 1024 ||
    p.startsWith("/") ||
    /[\\:]/.test(p) ||
    /\p{Cc}/u.test(p) ||
    p.split("/").some((seg) => seg === "" || seg === "." || seg === "..");
  return bad ? "Use a folder inside the repository, such as skills/pdf." : "";
}

/** refError mirrors ValidGitRef. */
export function refError(r: string): string {
  const bad =
    !r ||
    r.length > 255 ||
    r === "@" ||
    r.startsWith("-") ||
    r.startsWith("/") ||
    r.endsWith("/") ||
    r.endsWith(".") ||
    r.includes("..") ||
    r.includes("//") ||
    r.includes("@{") ||
    /[\s~^:?*[\\\p{Cc}]/u.test(r) ||
    r.split("/").some((seg) => seg.startsWith(".") || seg.endsWith(".lock"));
  return bad ? "Enter a branch, tag or commit." : "";
}

export const shortCommit = (c: string) => c.slice(0, 12);

/** repoLabel is owner/repo for GitHub, host/path otherwise. */
export function repoLabel(repo: string): string {
  try {
    const u = new URL(repo);
    const p = u.pathname.replace(/^\/|\/$/g, "").replace(/\.git$/, "");
    return u.host === "github.com" ? p : `${u.host}/${p}`;
  } catch {
    return repo;
  }
}

export const skillAgents = (packages: AgentPackage[]) =>
  packages.filter((p) => p.kind === "agent" && p.skills_dir);
export const unsupportedAgents = (packages: AgentPackage[]) =>
  packages.filter((p) => p.kind === "agent" && !p.skills_dir);

/** viaNotes names the agents that see a skill through another agent's directory. */
export function viaNotes(value: string[] | null, agents: AgentPackage[]): Record<string, string> {
  if (value === null) return {};
  const out: Record<string, string> = {};
  for (const a of agents) {
    if (value.includes(a.package)) continue;
    const via = agents.find((o) => value.includes(o.package) && a.skills_via?.includes(o.package));
    if (via) out[a.package] = `Through ${via.name}`;
  }
  return out;
}

/** reachLabel lists every agent a skill reaches, through also included. */
export function reachLabel(value: string[] | null, agents: AgentPackage[]): string {
  if (value === null) return "All agents";
  const via = viaNotes(value, agents);
  const names = agents
    .filter((a) => value.includes(a.package) || via[a.package])
    .map((a) => a.name);
  return names.length ? names.join(", ") : "No installed agent";
}

/** SkillView is the Skills table's layout: grouped by source, or one flat list. */
export type SkillView = "source" | "flat";
export const parseSkillView = (v: unknown): SkillView => (v === "flat" ? "flat" : "source");

type Forge = "github" | "gitlab" | "gitea";

/** forgeOf guesses a host's web layout; an unknown host gets no deep links. */
function forgeOf(host: string): Forge | null {
  if (host === "github.com") return "github";
  if (host.includes("gitlab")) return "gitlab";
  if (/gitea|forgejo|codeberg/.test(host)) return "gitea";
  return null;
}

/** repoHome is a repository's web page: its URL without a trailing / or .git. */
export const repoHome = (repo: string) => repo.replace(/\/+$/, "").replace(/\.git$/, "");

/** treeUrl is the web page of folder at commit, or null on an unknown host. */
export function treeUrl(repo: string, commit: string, folder: string): string | null {
  let host: string;
  try {
    host = new URL(repo).host;
  } catch {
    return null;
  }
  const p = folder ? `/${folder.split("/").map(encodeURIComponent).join("/")}` : "";
  const home = repoHome(repo);
  switch (forgeOf(host)) {
    case "github":
      return `${home}/tree/${commit}${p}`;
    case "gitlab":
      return `${home}/-/tree/${commit}${p}`;
    case "gitea":
      return `${home}/src/commit/${commit}${p}`;
    default:
      return null;
  }
}

/** commitUrl is the web page of a commit, or null on an unknown host. */
export function commitUrl(repo: string, commit: string): string | null {
  let host: string;
  try {
    host = new URL(repo).host;
  } catch {
    return null;
  }
  const forge = forgeOf(host);
  if (!forge) return null;
  return `${repoHome(repo)}${forge === "gitlab" ? "/-" : ""}/commit/${commit}`;
}
