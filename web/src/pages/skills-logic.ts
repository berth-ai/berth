import type { AgentPackage, Config, Skill } from "@/api/types";

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

/** sourceLabel is owner/repo for GitHub, host/path otherwise, then the path. */
export function sourceLabel(s: Skill): string {
  let repo = s.repo;
  try {
    const u = new URL(s.repo);
    const p = u.pathname.replace(/^\/|\/$/g, "").replace(/\.git$/, "");
    repo = u.host === "github.com" ? p : `${u.host}/${p}`;
  } catch {
    // Show the raw value.
  }
  return s.path ? `${repo} · ${s.path}` : repo;
}

export interface SkillRow {
  id: string;
  skill: Skill;
}

export function skillRows(cfg: Config): SkillRow[] {
  return Object.entries(cfg.skills ?? {})
    .map(([id, skill]) => ({ id, skill }))
    .sort((a, b) => a.id.localeCompare(b.id));
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

export const agentsPatch = (id: string, agents: string[] | null) => ({
  skills: { [id]: { agents } },
});
export const enabledPatch = (id: string, on: boolean) => ({ skills: { [id]: { enabled: on } } });
export const removePatch = (id: string) => ({ skills: { [id]: null } });
