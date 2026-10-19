import type { AgentPackage, InstructionFile, WorkspaceReport } from "@/api/types";

/** SHARED is the file every other instruction file links to when shared. */
export const SHARED = "AGENTS.md";

export interface InstructionRow {
  file: string;
  /** agents are the names of the enabled agents that read it. */
  agents: string[];
}

/** instructionRows lists the files the Instructions tab edits: each enabled
 * agent's file, and AGENTS.md while shared, which then every agent reads. */
export function instructionRows(packages: AgentPackage[], shared: boolean): InstructionRow[] {
  const agents = packages.filter((p) => p.kind === "agent" && p.enabled && p.instructions);
  const byFile = new Map<string, string[]>();
  for (const p of agents)
    byFile.set(p.instructions!, [...(byFile.get(p.instructions!) ?? []), p.name]);
  if (shared)
    byFile.set(
      SHARED,
      agents.map((p) => p.name),
    );
  return [...byFile.entries()]
    .map(([file, names]) => ({ file, agents: [...new Set(names)].sort() }))
    .sort((a, b) => a.file.localeCompare(b.file));
}

export type Tone = "ok" | "warn" | "muted";

/** fileState is the short state a file's card shows. */
export function fileState(f: InstructionFile): { label: string; tone: Tone } {
  if (f.state === "missing") return { label: "Not created yet", tone: "muted" };
  if (f.state === "symlink")
    return f.owned && f.target === SHARED
      ? { label: `Shares ${SHARED}`, tone: "ok" }
      : { label: `Link to ${f.target}`, tone: "muted" };
  if (!f.editable) return { label: f.reason ?? "Not editable here", tone: "warn" };
  if (f.tracked) return { label: "Tracked by git", tone: "muted" };
  return { label: "Not tracked by git", tone: "muted" };
}

/** mergeable lists the files shared instructions left alone because they
 * have their own content: each can be merged into AGENTS.md. */
export function mergeable(section: WorkspaceReport["instructions"]): string[] {
  return Object.entries(section?.skipped ?? {})
    .filter(([, why]) => why.endsWith("has its own content"))
    .map(([file]) => file)
    .sort();
}
