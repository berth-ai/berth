import type {
  AgentPackage,
  Attachment,
  Found,
  Job,
  SkillMode,
  NewSourceScan,
  Source,
  SourceDiff,
  SourceResult,
} from "@/api/types";
import { count } from "@/lib/format";
import { reachLabel, repoLabel, shortCommit } from "./skills-logic";

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

/** sourceLabel is owner/repo, then the folder when there is one. */
export const sourceLabel = (s: Pick<Source, "repo" | "path">) =>
  s.path ? `${repoLabel(s.repo)} · ${s.path}` : repoLabel(s.repo);

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
