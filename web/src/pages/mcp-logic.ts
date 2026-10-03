import type { AgentPackage, Config, McpAuth, McpServer } from "@/api/types";

const ID = /^[a-z0-9][a-z0-9-]{0,47}$/;
const LOOPBACK = /^(localhost|127(\.\d{1,3}){3}|\[::1\])$/;

export const AUTH_LABELS: Record<McpAuth, string> = {
  none: "None",
  bearer: "Bearer token",
  oauth: "OAuth",
};

/** tokenName is the agents.env key holding a bearer server's token. */
export function tokenName(id: string): string {
  return `MCP_${id.toUpperCase().replaceAll("-", "_")}_TOKEN`;
}

export function idError(id: string, taken: string[]): string {
  if (!id) return "Enter an id.";
  if (!ID.test(id))
    return "Use 1 to 48 lowercase letters, digits and dashes, starting with a letter or digit.";
  if (taken.includes(id)) return `${id} is already in use.`;
  return "";
}

/** urlError mirrors the broker's checkMCPURL. */
export function urlError(raw: string): string {
  const s = raw.trim();
  if (!s) return "Enter the server's URL.";
  if (s.length > 2048) return "Use at most 2048 characters.";
  let u: URL;
  try {
    u = new URL(s);
  } catch {
    return "Enter an absolute URL such as https://example.com/mcp.";
  }
  if (u.username || u.password)
    return "Leave the user name and password out; use bearer auth for tokens.";
  if (u.protocol === "https:") return "";
  if (u.protocol === "http:")
    return LOOPBACK.test(u.hostname) ? "" : "Use https unless the host is localhost.";
  return "Start the URL with https://.";
}

export type McpRow =
  | { kind: "lock"; id: string; name: string; pkg: AgentPackage }
  | {
      kind: "user";
      id: string;
      name: string;
      server: McpServer;
      clash: boolean;
      tokenSet: boolean;
    };

/** mcpRows lists the lock's MCP packages, then the user's servers, each by name. */
export function mcpRows(
  packages: AgentPackage[],
  cfg: Config,
  secrets: Record<string, { set: boolean }>,
): McpRow[] {
  const byName = (a: McpRow, b: McpRow) => a.name.localeCompare(b.name);
  const lock: McpRow[] = packages
    .filter((p) => p.kind === "mcp")
    .map((p) => ({ kind: "lock" as const, id: p.mcp_id ?? "", name: p.name, pkg: p }))
    .sort(byName);
  const lockIds = new Set(lock.map((r) => r.id).filter(Boolean));
  const user: McpRow[] = Object.entries(cfg.mcp?.servers ?? {})
    .map(([id, server]) => ({
      kind: "user" as const,
      id,
      name: id,
      server,
      clash: lockIds.has(id),
      tokenSet: secrets[tokenName(id)]?.set ?? false,
    }))
    .sort(byName);
  return [...lock, ...user];
}

export function availableLabel(list: string[] | undefined, agents: AgentPackage[]): string {
  if (!list) return "All agents";
  const names = agents.filter((a) => list.includes(a.package)).map((a) => a.name);
  return names.length ? names.join(", ") : "No installed agent";
}

/** pickedAfter toggles one agent; every agent ticked means all agents (null). It never empties the list. */
export function pickedAfter(
  cur: string[] | null,
  all: string[],
  pkg: string,
  on: boolean,
): string[] | null {
  const set = new Set(cur ?? all);
  if (on) set.add(pkg);
  else set.delete(pkg);
  if (set.size === 0) return cur;
  if (all.every((a) => set.has(a))) return null;
  return [...set].sort();
}

export const serverPatch = (id: string, server: McpServer, available: string[] | null) => ({
  mcp: { servers: { [id]: server }, available: { [id]: available } },
});
export const availablePatch = (id: string, available: string[] | null) => ({
  mcp: { available: { [id]: available } },
});
export const enabledPatch = (id: string, on: boolean) => ({
  mcp: { servers: { [id]: { enabled: on } } },
});
export const removePatch = (id: string) => ({
  mcp: { servers: { [id]: null }, available: { [id]: null } },
});
