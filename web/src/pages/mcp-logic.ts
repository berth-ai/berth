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

/** ALL_AGENTS, alone in a config list, makes a server reach every agent,
 * including ones added later (config.AllAgents in Go). */
export const ALL_AGENTS = "*";

/** reachOf reads a server's config list as the pickers hold it: null is
 * every agent, and a missing list is no agent. */
export const reachOf = (list: string[] | null | undefined): string[] | null =>
  list?.includes(ALL_AGENTS) ? null : (list ?? []);

/** reachValue is the config list for a picker value; no agent removes it. */
export const reachValue = (v: string[] | null): string[] | null =>
  v === null ? [ALL_AGENTS] : v.length ? [...v].sort() : null;

export function availableLabel(value: string[] | null, agents: AgentPackage[]): string {
  if (value === null) return "All agents";
  if (!value.length) return "No agent";
  const names = agents.filter((a) => value.includes(a.package)).map((a) => a.name);
  return names.length ? names.join(", ") : "No installed agent";
}

export const serverPatch = (id: string, server: McpServer, reach: string[] | null) => ({
  mcp: { servers: { [id]: server }, available: { [id]: reachValue(reach) } },
});
export const availablePatch = (id: string, reach: string[] | null) => ({
  mcp: { available: { [id]: reachValue(reach) } },
});
export const enabledPatch = (id: string, on: boolean) => ({
  mcp: { servers: { [id]: { enabled: on } } },
});
export const removePatch = (id: string) => ({
  mcp: { servers: { [id]: null }, available: { [id]: null }, workspaces: { [id]: null } },
});
