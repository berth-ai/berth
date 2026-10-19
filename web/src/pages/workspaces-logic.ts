import type { AgentPackage, Config, Workspace, WorkspaceState } from "@/api/types";
import { count } from "@/lib/format";
import { pathError } from "./skills-logic";

export interface WorkspaceRow {
  id: string;
  ws: Workspace;
}

export function workspaceRows(cfg: Config | undefined): WorkspaceRow[] {
  return Object.entries(cfg?.workspaces ?? {})
    .map(([id, ws]) => ({ id, ws }))
    .sort((a, b) => a.id.localeCompare(b.id));
}

/** defaultPath is owner/repo of a repository URL: its last two path
 * segments, without .git. */
export function defaultPath(repo: string): string {
  try {
    const parts = new URL(repo).pathname
      .replace(/\.git$/, "")
      .split("/")
      .filter(Boolean);
    return parts.slice(-2).join("/");
  } catch {
    return "";
  }
}

/** deriveId turns a folder into a workspace id: acme/api → acme-api. */
export function deriveId(path: string): string {
  return path
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64)
    .replace(/-+$/, "");
}

/** idError mirrors config.WorkspaceIDPattern and the broker's id_exists. */
export function idError(id: string, cfg: Config | undefined): string {
  if (!/^[a-z0-9][a-z0-9-]{0,63}$/.test(id))
    return "Use 1 to 64 lowercase letters, digits and dashes, starting with a letter or digit.";
  if (cfg?.workspaces?.[id]) return `A workspace named ${id} exists; choose another id.`;
  return "";
}

export const overlaps = (a: string, b: string) =>
  a === b || a.startsWith(`${b}/`) || b.startsWith(`${a}/`);

/** folderError mirrors config.CheckWorkspacePath and the broker's
 * path_taken and nested checks. */
export function folderError(path: string, cfg: Config | undefined): string {
  if (
    !path ||
    pathError(path) ||
    path.split("/").some((s) => s === ".git" || s.startsWith(".berth-tmp-"))
  )
    return "Use a folder inside /workspaces, such as acme/api.";
  for (const { id, ws } of workspaceRows(cfg)) {
    if (ws.path === path) return `Workspace ${id} already uses this folder.`;
    if (overlaps(ws.path, path))
      return `This folder overlaps workspace ${id} (${ws.path}); workspaces cannot nest.`;
  }
  return "";
}

export const sessionName = (id: string) => `ws-${id}`;
/** terminalPath opens ttyd on the workspace's session; the proxy passes arg on. */
export const terminalPath = (id: string) => `/terminal/?arg=${sessionName(id)}`;

/** launchAgents are the agents a workspace can launch. */
export const launchAgents = (packages: AgentPackage[]) =>
  packages.filter((p) => p.kind === "agent" && p.enabled && (p.bins?.length ?? 0) > 0 && !p.error);

/** gitSummary is the branch and the number of changes, for the list. */
export function gitSummary(st: WorkspaceState | undefined): string {
  if (!st || st.unknown) return "Unknown";
  if (!st.exists) return "Folder missing";
  if (!st.git) return "Not a git repository";
  const where = st.branch || (st.head ? `detached at ${st.head.slice(0, 7)}` : "no commits");
  return st.dirty > 0 ? `${where} · ${count(st.dirty, "change", "changes")}` : where;
}

export const WORKSPACE_TABS = [
  "overview",
  "terminal",
  "skills",
  "mcp",
  "instructions",
  "env",
] as const;
export type WorkspaceTab = (typeof WORKSPACE_TABS)[number];
export const parseWorkspaceTab = (v: unknown): WorkspaceTab =>
  WORKSPACE_TABS.find((t) => t === v) ?? "overview";
