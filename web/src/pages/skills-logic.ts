import type { AgentPackage, Config, Job, ScanResult, ScanRow, Skill } from "@/api/types";
import { count } from "@/lib/format";
import { pickedAfter } from "./mcp-logic";

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

/** sourceLabel is the repository label, then the path. */
export function sourceLabel(s: Skill): string {
  const repo = repoLabel(s.repo);
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

/** SkillGroup is the installed skills sharing a repository and ref. */
export interface SkillGroup {
  key: string;
  repo: string;
  ref: string;
  rows: SkillRow[];
}

/** skillGroups groups skills by repository and ref, sorted by repository,
 * ref, then name. Groups are computed, never stored. */
export function skillGroups(cfg: Config): SkillGroup[] {
  const groups = new Map<string, SkillGroup>();
  for (const row of skillRows(cfg)) {
    const key = `${row.skill.repo}\n${row.skill.ref}`;
    const group = groups.get(key) ?? { key, repo: row.skill.repo, ref: row.skill.ref, rows: [] };
    group.rows.push(row);
    groups.set(key, group);
  }
  return [...groups.values()].sort(
    (a, b) => a.repo.localeCompare(b.repo) || a.ref.localeCompare(b.ref),
  );
}

/** groupOf is the rows of the group holding id. */
export const groupOf = (groups: SkillGroup[], id: string): SkillRow[] =>
  groups.find((g) => g.rows.some((r) => r.id === id))?.rows ?? [];

/** scanOf reads a finished skills.scan job, or null if it holds no scan. */
export function scanOf(job: Job): ScanResult | null {
  const r = job.result;
  return r?.commit ? { commit: r.commit, skills: (r.skills ?? []) as ScanRow[] } : null;
}

/** selectable says whether a scanned skill can be added. */
export const selectable = (r: ScanRow) => !r.error && !r.installed;
/** defaultPicks are the paths of every scanned skill that can be added. */
export const defaultPicks = (rows: ScanRow[]) => rows.filter(selectable).map((r) => r.path);
/** rowNote says why a scanned skill cannot be added, or "". */
export const rowNote = (r: ScanRow) => r.error ?? (r.installed ? "Already installed" : "");

/** updateText is the toast for a skills.update job that succeeded. */
export function updateText(job: Job, bg: string): string {
  const ids = (job.result?.skills ?? []) as string[];
  if (!job.result?.changed || ids.length === 0) return "Already up to date.";
  return ids.length === 1
    ? `${ids[0]} updated.${bg}`
    : `${count(ids.length, "skill", "skills")} updated.${bg}`;
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

/** SkillView is the Skills table's layout: grouped by source, or one flat list. */
export type SkillView = "source" | "flat";
export const parseSkillView = (v: unknown): SkillView => (v === "flat" ? "flat" : "source");

/** flatRows is every installed skill, by name. */
export const flatRows = (groups: SkillGroup[]): SkillRow[] =>
  groups.flatMap((g) => g.rows).sort((a, b) => a.id.localeCompare(b.id));

/** drifted is how many commits a group's skills sit on, or 0 if they agree. */
export function drifted(rows: SkillRow[]): number {
  const n = new Set(rows.map((r) => r.skill.commit)).size;
  return n > 1 ? n : 0;
}

const picks = (value: string[] | null, pkg: string) => value === null || value.includes(pkg);

/** groupReach counts, per agent, the skills in rows that pick it directly. */
export const groupReach = (rows: SkillRow[], agents: AgentPackage[]) =>
  agents.map((agent) => ({
    agent,
    n: rows.filter((r) => picks(r.skill.agents, agent.package)).length,
  }));

/** usedBy names the agents a skill, or every skill of a group, reaches. */
export function usedBy(rows: SkillRow[], agents: AgentPackage[]): string {
  if (rows.every((r) => r.skill.agents === null)) return "All agents";
  const labels = new Set(rows.map((r) => reachLabel(r.skill.agents, agents)));
  return labels.size === 1 ? [...labels][0] : "Varies by skill";
}

/** enabledState is a group's switch: on only when every skill is on. */
export function enabledState(rows: SkillRow[]): { on: boolean; word: string } {
  const n = rows.filter((r) => r.skill.enabled).length;
  if (n === rows.length) return { on: true, word: "On" };
  return { on: false, word: n ? `${n} of ${rows.length}` : "Off" };
}

const each = (rows: SkillRow[], fn: (r: SkillRow) => object | null) => ({
  skills: Object.fromEntries(rows.map((r) => [r.id, fn(r)])),
});
export const groupEnabledPatch = (rows: SkillRow[], on: boolean) =>
  each(rows, () => ({ enabled: on }));
export const groupRemovePatch = (rows: SkillRow[]) => each(rows, () => null);
/** groupAgentsPatch gives, or takes, one agent for every skill in rows. */
export const groupAgentsPatch = (rows: SkillRow[], all: string[], pkg: string, on: boolean) =>
  each(rows, (r) => ({ agents: pickedAfter(r.skill.agents, all, pkg, on) }));

/** commonDir is the deepest folder holding every one of paths' parents. */
export function commonDir(paths: string[]): string {
  const dirs = paths.map((p) => p.split("/").slice(0, -1));
  const out: string[] = [];
  for (let i = 0; dirs.every((d) => i < d.length && d[i] === dirs[0][i]); i++) out.push(dirs[0][i]);
  return out.join("/");
}

/** skillLocation is where the broker keeps a skill's folder (skills.StoreDir). */
export const skillLocation = (id: string) => `~/.local/share/berth/skills/${id}`;
