import type {
  AgentPackage,
  Attachment,
  Found,
  SkillMode,
  Source,
  WorkspaceMcpFile,
  WorkspaceReport,
  WorkspaceSkill,
} from "@/api/types";
import { availableLabel } from "./mcp-logic";
import { agentsOf, type Sources, sourceList, whoText } from "./sources-logic";

export const MODE_LABELS: Record<SkillMode, string> = { link: "Link", vendor: "Vendor" };

/** workspacesOf is where an attachment reaches, by workspace id. */
export const workspacesOf = (a: Attachment | null | undefined): Record<string, SkillMode> =>
  a?.workspaces ?? {};

/** wsConflicts maps each workspace another source already gives name to,
 * to that source (one provider per name per workspace). */
export function wsConflicts(
  sources: Sources,
  sourceId: string,
  name: string,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const { id, source } of sourceList(sources)) {
    if (id === sourceId) continue;
    for (const ws of Object.keys(workspacesOf(source.attached?.[name]))) out[ws] ??= id;
  }
  return out;
}

/** targetsText names who an attachment reaches: its agents, then its
 * workspaces, vendored ones marked. */
export function targetsText(a: Attachment, agents: AgentPackage[]): string {
  const ws = Object.entries(workspacesOf(a))
    .sort(([x], [y]) => x.localeCompare(y))
    .map(([id, mode]) => (mode === "vendor" ? `${id} (vendored)` : id));
  const picks = agentsOf(a);
  const who = picks !== null && picks.length === 0 && ws.length ? "" : whoText(a, agents);
  return [who, ws.join(", ")].filter(Boolean).join(" · ");
}

const at = (sourceId: string, name: string, v: object | null) => ({
  skills: { sources: { [sourceId]: { attached: { [name]: v } } } },
});

/** wsPatch attaches name to workspace ws in mode, or detaches it from ws
 * (mode null). A new attachment reaches only ws; one left reaching
 * nothing is removed. */
export function wsPatch(
  sourceId: string,
  name: string,
  ws: string,
  mode: SkillMode | null,
  current: Attachment | null,
) {
  if (mode)
    return at(
      sourceId,
      name,
      current
        ? { workspaces: { [ws]: mode } }
        : { enabled: true, agents: [], workspaces: { [ws]: mode } },
    );
  const picks = current ? agentsOf(current) : [];
  const others = Object.keys(workspacesOf(current)).filter((w) => w !== ws);
  if (picks !== null && picks.length === 0 && others.length === 0) return at(sourceId, name, null);
  return at(sourceId, name, { workspaces: { [ws]: null } });
}

export interface WsSkillRow {
  key: string;
  sourceId: string;
  name: string;
  source: Source;
  attachment: Attachment;
  mode: SkillMode;
  found: Found | null;
  missing: boolean;
}

/** workspaceSkills lists the skills attached to workspace ws, across
 * sources, by name then source. */
export const workspaceSkills = (sources: Sources, ws: string): WsSkillRow[] =>
  sourceList(sources)
    .flatMap(({ id, source }) =>
      Object.entries(source.attached ?? {}).flatMap(([name, attachment]) => {
        const mode = workspacesOf(attachment)[ws];
        if (!mode) return [];
        const found = source.found?.[name] ?? null;
        return [
          {
            key: `${id}/${name}`,
            sourceId: id,
            name,
            source,
            attachment,
            mode,
            found,
            missing: !found || Boolean(found.error),
          },
        ];
      }),
    )
    .sort((a, b) => a.name.localeCompare(b.name) || a.sourceId.localeCompare(b.sourceId));

export interface StrandedRow {
  key: string;
  sourceId: string;
  entry: WorkspaceSkill;
}

/** strandedSkills lists the report's entries no attachment explains any
 * more: detached, but kept because of local edits. */
export function strandedSkills(
  report: WorkspaceReport | undefined,
  rows: WsSkillRow[],
): StrandedRow[] {
  const known = new Set(rows.map((r) => r.key));
  return Object.entries(report?.skills.entries ?? {})
    .filter(([key]) => !known.has(key))
    .map(([key, entry]) => ({ key, sourceId: key.split("/")[0] ?? "", entry }))
    .sort((a, b) => a.key.localeCompare(b.key));
}

export interface SkillState {
  label: string;
  tone: "ok" | "warn" | "info" | "idle";
  edited: boolean;
  detail?: string;
}

/** skillState says what Berth did with an attached skill in a workspace,
 * from its last report. */
export function skillState(row: WsSkillRow, report: WorkspaceReport | undefined): SkillState {
  if (!row.attachment.enabled) return { label: "Off", tone: "idle", edited: false };
  const entry = report?.skills.entries?.[row.key];
  if (entry?.edited)
    return {
      label: "Local edits",
      tone: "warn",
      edited: true,
      detail: `${entry.dir ?? row.name} differs from the copy Berth made; Berth leaves it as it is.`,
    };
  if (row.missing) return { label: "Missing from source", tone: "warn", edited: false };
  const skipped = Object.entries(report?.skills.skipped ?? {}).find(([p]) =>
    p.endsWith(`/${row.name}`),
  );
  if (entry && entry.mode === row.mode) {
    const where = [entry.dir, ...(entry.links ?? [])].filter(Boolean).join(", ");
    return {
      label: row.mode === "vendor" ? "Vendored" : "Linked",
      tone: "ok",
      edited: false,
      detail: where,
    };
  }
  if (skipped)
    return {
      label: "Already in the folder",
      tone: "warn",
      edited: false,
      detail: `${skipped[0]}: ${skipped[1]}`,
    };
  return {
    label: "Waiting",
    tone: "info",
    edited: false,
    detail: "Berth places it in the background.",
  };
}

export interface FindRow {
  sourceId: string;
  name: string;
  found: Found;
  source: Source;
  taken: string; // the other source that gives name to the workspace, or ""
  attached: boolean; // this source already gives it to the workspace
}

/** findable lists every valid found skill whose name or description
 * matches query, for the Attach skill dialog, by name then source. */
export function findable(sources: Sources, ws: string, query: string): FindRow[] {
  const q = query.trim().toLowerCase();
  return sourceList(sources)
    .flatMap(({ id, source }) =>
      Object.entries(source.found ?? {})
        .filter(
          ([name, f]) =>
            !f.error && (!q || name.includes(q) || (f.description ?? "").toLowerCase().includes(q)),
        )
        .map(([name, found]) => ({
          sourceId: id,
          name,
          found,
          source,
          taken: wsConflicts(sources, id, name)[ws] ?? "",
          attached: Boolean(workspacesOf(source.attached?.[name])[ws]),
        })),
    )
    .sort((a, b) => a.name.localeCompare(b.name) || a.sourceId.localeCompare(b.sourceId));
}

/** toggled adds or removes ws from a server's sorted workspace list. */
export function toggled(list: string[] | undefined, ws: string, on: boolean): string[] {
  const set = new Set(list ?? []);
  if (on) set.add(ws);
  else set.delete(ws);
  return [...set].sort();
}

/** mcpWorkspacesPatch sets a server's workspaces; none removes the entry. */
export const mcpWorkspacesPatch = (server: string, list: string[]) => ({
  mcp: { workspaces: { [server]: list.length ? [...list].sort() : null } },
});

/** mcpTargetsText names a server's agents, then its workspaces. */
export const mcpTargetsText = (
  available: string[] | undefined,
  workspaces: string[] | undefined,
  agents: AgentPackage[],
) => [availableLabel(available, agents), (workspaces ?? []).join(", ")].filter(Boolean).join(" · ");

/** mcpFileNote says how a project file relates to git. */
export function mcpFileNote(f: WorkspaceMcpFile): string {
  if (f.created) return "Created by Berth, hidden from git.";
  if (f.tracked) return "Tracked by git: Berth's entries show in git diff.";
  return "Your file: Berth merges its entries and leaves the rest.";
}

/** resolvedText is the toast after Overwrite or Stop managing. */
export const resolvedText = (action: "overwrite" | "release", name: string, ws: string) =>
  action === "overwrite"
    ? `Berth replaces ${name} in ${ws} with the source's copy in the background.`
    : `${name} in ${ws} is yours now; Berth no longer manages it.`;

/** trustNote names the enabled agents that trust a workspace whose
 * project file Berth writes (Codex), or "". */
export function trustNote(agents: AgentPackage[]): string {
  const names = agents.filter((a) => a.enabled && a.mcp_trust).map((a) => a.name);
  return names.length ? `${names.join(" and ")} will trust each workspace you tick.` : "";
}
