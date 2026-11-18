import type {
  AgentPackage,
  Attachment,
  BulkUpdateResult,
  Found,
  Job,
  SkillMode,
  NewSourceScan,
  Source,
  SourceCheck,
  SourceDiff,
  SourceResult,
} from "@/api/types";
import { count } from "@/lib/format";
import type { Tone } from "@/lib/notify";
import { reachLabel, shortCommit, sourceTitle } from "./skills-logic";

/** SUGGESTED is the source the Skills page offers until it is added. */
export const SUGGESTED = {
  repo: "https://github.com/anthropics/skills",
  ref: "main",
  path: "skills",
  label: "anthropics/skills",
  description: "Anthropic's skills for documents, PDFs, spreadsheets, slides and more.",
} as const;

export type Sources = Record<string, Source> | null | undefined;

export interface SourceEntry {
  id: string;
  source: Source;
}

/** sourceList is every source, by id. */
export const sourceList = (sources: Sources): SourceEntry[] =>
  Object.entries(sources ?? {})
    .map(([id, source]) => ({ id, source }))
    .sort((a, b) => a.id.localeCompare(b.id));

/** hasSuggested says whether a source already tracks the suggested repository. */
export const hasSuggested = (sources: Sources) =>
  sourceList(sources).some((e) => e.source.repo === SUGGESTED.repo);

/** sourceLabel is owner/repo (a website's host), then the folder when there is one. */
export const sourceLabel = (s: Pick<Source, "kind" | "repo" | "path">) =>
  s.path ? `${sourceTitle(s)} · ${s.path}` : sourceTitle(s);

/** agentsOf is who an attachment reaches: null is every agent, and so is
 * a missing key (the devserver drops nulls). */
export const agentsOf = (a: Attachment): string[] | null => a.agents ?? null;

export interface FoundRow {
  name: string;
  found: Found;
  attachment: Attachment | null;
}

/** foundRows is a source's last scan, by name, with each row's attachment. */
export const foundRows = (s: Source): FoundRow[] =>
  Object.entries(s.found ?? {})
    .map(([name, found]) => ({ name, found, attachment: s.attached?.[name] ?? null }))
    .sort((a, b) => a.name.localeCompare(b.name));

const isMissing = (s: Source, name: string) => {
  const f = s.found?.[name];
  return !f || Boolean(f.error);
};

/** missingNames lists the attached names the source no longer holds as a
 * valid skill (Source.Missing in Go). */
export const missingNames = (s: Source): string[] =>
  Object.keys(s.attached ?? {})
    .filter((n) => isMissing(s, n))
    .sort();

/** sourceSummary is "N skills · M attached". */
export function sourceSummary(s: Source): string {
  const skills = Object.values(s.found ?? {}).filter((f) => !f.error).length;
  return `${count(skills, "skill", "skills")} · ${Object.keys(s.attached ?? {}).length} attached`;
}

export interface AttachedRow {
  name: string;
  id: string;
  source: Source;
  attachment: Attachment;
  found: Found | null;
  missing: boolean;
}

/** attachedRows is every attached skill across sources, by name. */
export const attachedRows = (sources: Sources): AttachedRow[] =>
  sourceList(sources)
    .flatMap(({ id, source }) =>
      Object.entries(source.attached ?? {}).map(([name, attachment]) => ({
        name,
        id,
        source,
        attachment,
        found: source.found?.[name] ?? null,
        missing: isMissing(source, name),
      })),
    )
    .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));

/** conflicts maps each agent package another source already gives name
 * to, to that source; "*" means another source gives it to every agent
 * (config.ProviderOf in Go). */
export function conflicts(
  sources: Sources,
  sourceId: string,
  name: string,
  agents: AgentPackage[],
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const { id, source } of sourceList(sources)) {
    const a = source.attached?.[name];
    if (id === sourceId || !a) continue;
    const picks = agentsOf(a);
    if (picks === null) {
      out["*"] ??= id;
      for (const ag of agents) out[ag.package] ??= id;
    } else for (const p of picks) out[p] ??= id;
  }
  return out;
}

/** pickAgent ticks or unticks one agent. A list stays a list even with
 * every agent ticked: "including ones added later" is its own choice. */
export function pickAgent(value: string[] | null, all: string[], pkg: string, on: boolean) {
  const set = new Set(value ?? all);
  if (on) set.add(pkg);
  else set.delete(pkg);
  return [...set].sort();
}

const at = (id: string, name: string, v: object | null) => ({
  skills: { sources: { [id]: { attached: { [name]: v } } } },
});

/** detachPatch removes an attachment. */
export const detachPatch = (id: string, name: string) => at(id, name, null);

const sameModes = (a: Record<string, SkillMode>, b: Record<string, SkillMode>) =>
  Object.keys(a).length === Object.keys(b).length &&
  Object.entries(a).every(([k, v]) => b[k] === v);

/** attachPatch attaches name to agents and workspaces, or changes who
 * gets it; workspaces default to the current ones. No agent and no
 * workspace detaches it. */
export function attachPatch(
  id: string,
  name: string,
  agents: string[] | null,
  current: Attachment | null,
  workspaces: Record<string, SkillMode> = current?.workspaces ?? {},
) {
  if (agents !== null && agents.length === 0 && Object.keys(workspaces).length === 0)
    return detachPatch(id, name);
  if (!current) return at(id, name, { enabled: true, agents, workspaces });
  const prev = current.workspaces ?? {};
  if (sameModes(prev, workspaces)) return at(id, name, { agents });
  const ws: Record<string, SkillMode | null> = { ...workspaces };
  for (const w of Object.keys(prev)) if (!(w in workspaces)) ws[w] = null;
  return at(id, name, { agents, workspaces: ws });
}

/** enabledPatch switches an attachment on or off. */
export const enabledPatch = (id: string, name: string, on: boolean) =>
  at(id, name, { enabled: on });

/** whoText names who an attachment reaches, through other agents' folders included. */
export function whoText(a: Attachment, agents: AgentPackage[]): string {
  const picks = agentsOf(a);
  if (picks === null) return "All agents";
  if (picks.length === 0) return "No agent";
  return reachLabel(picks, agents);
}

/** diffOf reads a finished preview of an existing source, or null. */
export function diffOf(job: Job): SourceDiff | null {
  const r = job.result as unknown as Partial<SourceDiff> | null;
  return r?.commit && Array.isArray(r.added) ? (r as SourceDiff) : null;
}

/** newScanOf reads a finished preview of a repository not added yet, or null. */
export function newScanOf(job: Job): NewSourceScan | null {
  const r = job.result as unknown as Partial<NewSourceScan> | null;
  return r?.commit && r.found && typeof r.id === "string" ? (r as NewSourceScan) : null;
}

/** sourceResult reads what skills.sources.add or .update answered. */
export const sourceResult = (job: Job): SourceResult => (job.result ?? {}) as SourceResult;

/** dropsAttached lists the attached skills an update would leave missing. */
export const dropsAttached = (d: SourceDiff): string[] =>
  [...d.removed.filter((r) => r.attached), ...d.described.filter((r) => r.attached && r.error)]
    .map((r) => r.name)
    .sort();

/** addedText is the toast after skills.sources.add. */
export function addedText(r: SourceResult): string {
  const n = r.attached?.length ?? 0;
  const skipped = r.skipped?.length
    ? ` Skipped ${r.skipped.join(", ")}: another source already provides ${r.skipped.length === 1 ? "it" : "them"}.`
    : "";
  return n
    ? `${r.id} added; ${count(n, "skill", "skills")} attached to all agents.${skipped}`
    : `${r.id} added.${skipped}`;
}

/** updatedText is the toast after skills.sources.update. */
export function updatedText(r: SourceResult): string {
  if (!r.changed) return "Already up to date.";
  const m = r.missing ?? [];
  const tail = m.length
    ? ` ${m.join(", ")} ${m.length === 1 ? "is" : "are"} no longer in the source; detach ${m.length === 1 ? "it" : "them"} on its page.`
    : "";
  return `${r.id} updated to ${shortCommit(r.commit ?? "")}.${tail}`;
}

/** sourceIdError mirrors config.WorkspaceIDPattern. */
export const sourceIdError = (id: string) =>
  /^[a-z0-9][a-z0-9-]{0,63}$/.test(id) ? "" : "Use 1 to 64 lowercase letters, digits and dashes.";

/** exportPath is where the agent's home keeps an attached skill (skills.StoreDir). */
export const exportPath = (id: string, name: string) => `~/.local/share/berth/skills/${id}/${name}`;

/** SkillFilter narrows a source's skills: every one, or the attached ones. */
export type SkillFilter = "all" | "attached";

/** filterRows keeps the rows whose name or description matches query and,
 * for "attached", that are attached. */
export function filterRows(rows: FoundRow[], query: string, filter: SkillFilter): FoundRow[] {
  const q = query.trim().toLowerCase();
  return rows.filter(
    (r) =>
      (filter === "all" || r.attachment) &&
      (!q || r.name.includes(q) || (r.found.description ?? "").toLowerCase().includes(q)),
  );
}

/** CheckState is what the last check found for a source as it is now. */
export type CheckState =
  | { kind: "behind"; check: SourceCheck }
  | { kind: "current" }
  | { kind: "failed"; message: string };

/** checkState reads a source's row of the last check; null when there was
 * none, or the source moved to another ref or commit since. */
export function checkState(source: Source, check: SourceCheck | undefined): CheckState | null {
  if (!check || check.ref !== source.ref) return null;
  if (check.error)
    return check.from === source.commit
      ? { kind: "failed", message: check.error.message ?? check.error.code }
      : null;
  if (check.commit === source.commit) return { kind: "current" };
  return check.from === source.commit ? { kind: "behind", check } : null;
}

/** behindSources lists the sources the last check found a newer commit for. */
export function behindSources(sources: Sources, checks: SourceCheck[] | undefined): SourceCheck[] {
  const byId = new Map((checks ?? []).map((c) => [c.id, c]));
  return sourceList(sources).flatMap(({ id, source }) => {
    const st = checkState(source, byId.get(id));
    return st?.kind === "behind" ? [st.check] : [];
  });
}

/** checksOf reads a finished skills.sources.check, or null. */
export function checksOf(job: Job): SourceCheck[] | null {
  const r = job.result as unknown as { sources?: SourceCheck[] } | null;
  return Array.isArray(r?.sources) ? r.sources : null;
}

/** checkSummary says what a check found. */
export function checkSummary(checks: SourceCheck[]): { tone: Tone; text: string } {
  const failed = checks.filter((c) => c.error).length;
  const behind = checks.filter((c) => !c.error && c.commit !== c.from).length;
  const notChecked = failed ? ` ${count(failed, "source", "sources")} could not be checked.` : "";
  if (!checks.length) return { tone: "info", text: "There are no sources to check." };
  if (behind)
    return {
      tone: "warn",
      text: `${count(behind, "source has", "sources have")} new commits.${notChecked}`,
    };
  return failed
    ? { tone: "warn", text: `Everything else is up to date.${notChecked}` }
    : { tone: "ok", text: "Every source is up to date." };
}

/** changeCounts is "2 added · 1 removed" for a preview, or "". */
export function changeCounts(d: SourceDiff): string {
  const parts: [number, string][] = [
    [d.added.length, "added"],
    [d.removed.length, "removed"],
    [d.moved.length, "moved"],
    [d.described.length, "reworded"],
  ];
  return parts
    .filter(([n]) => n)
    .map(([n, what]) => `${n} ${what}`)
    .join(" · ");
}

/** bulkResult reads what skills.sources.update_all answered. */
export const bulkResult = (job: Job): BulkUpdateResult => (job.result ?? {}) as BulkUpdateResult;

/** updatedAllText is the toast after skills.sources.update_all. */
export function updatedAllText(r: BulkUpdateResult): string {
  const updated = r.updated ?? [];
  const missing = [...new Set(updated.flatMap((u) => u.missing ?? []))].sort();
  const failed = r.failed ?? [];
  const parts: string[] = [];
  if (updated.length) parts.push(`Updated ${updated.map((u) => u.id).join(", ")}.`);
  else if (!failed.length) parts.push("Already up to date.");
  if (missing.length)
    parts.push(
      `${missing.join(", ")} ${missing.length === 1 ? "is" : "are"} no longer in ${missing.length === 1 ? "its source" : "their sources"}; detach ${missing.length === 1 ? "it" : "them"} on the source's page.`,
    );
  if (failed.length)
    parts.push(
      `Could not update ${failed.map((f) => f.id).join(", ")}; open Activity for the log.`,
    );
  return parts.join(" ");
}
