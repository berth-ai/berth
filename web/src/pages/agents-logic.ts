import type {
  AgentPackage,
  AgentUsage,
  LoginMethodStatus,
  PackageKey,
  RegistryAgent,
  UpdateCheck,
} from "@/api/types";
import { short, since, tokens } from "@/lib/format";
import type { Tone } from "@/lib/notify";

export function checkResult(c: UpdateCheck): { tone: Tone; text: string } {
  if (c.update_available === null)
    return { tone: "warn", text: `Could not reach ${c.ref}: ${c.error}` };
  if (c.update_available)
    return { tone: "warn", text: `Update available on ${c.ref}: ${short(c.hash)}.` };
  return { tone: "ok", text: `Up to date with ${c.ref}.` };
}

/** disabledAfter is agents.disabled once pkg is switched on or off. */
export function disabledAfter(all: AgentPackage[], pkg: string, on: boolean): string[] {
  const disabled = new Set(all.filter((p) => !p.enabled).map((p) => p.package));
  if (on) disabled.delete(pkg);
  else disabled.add(pkg);
  return [...disabled];
}

export function versionLabel(hash: string, current?: string, previous?: string): string {
  if (hash === current) return "Current";
  if (hash === previous) return "Previous";
  return "Kept";
}

export function keyBadge(k: PackageKey): "set" | "not_set" {
  return k.set ? "set" : "not_set";
}

/** allAdded hides Add agent once every registry entry is added. */
export function allAdded(entries: RegistryAgent[] | undefined): boolean {
  return entries !== undefined && entries.every((e) => e.added);
}

/** agentHome is the folder an agent keeps its own settings in, from its
 * skills directory: ~/.copilot/skills is ~/.copilot. */
export function agentHome(skillsDir: string | undefined): string {
  const m = skillsDir?.match(/^(~\/[^/]+|\$XDG_CONFIG_HOME\/[^/]+)\//);
  return m ? m[1] : "its own folder";
}

export const addedToast = (name: string) => `${name} added. Sign in from its page.`;

export const removeBody = (p: Pick<AgentPackage, "skills_dir">) =>
  `Berth deletes its install. Its settings and sign-in in ${agentHome(p.skills_dir)} stay.`;

const bare = (pkg: string) => pkg.slice(pkg.lastIndexOf("/") + 1);

/** agentSlug names an agent in its page's URL: the package name without
 * its scope (claude-code), or scope~name when two agents share a name. npm
 * names never hold "~", so the long form stays unambiguous. */
export function agentSlug(pkg: string, all: Pick<AgentPackage, "package">[]): string {
  const name = bare(pkg);
  const shared = all.some((p) => p.package !== pkg && bare(p.package) === name);
  return shared ? pkg.replace(/^@/, "").replace("/", "~") : name;
}

/** findAgent is the agent whose page slug names, if any. */
export function findAgent<T extends Pick<AgentPackage, "package" | "kind">>(
  all: T[],
  slug: string,
): T | undefined {
  const agents = all.filter((p) => p.kind === "agent");
  return agents.find((p) => agentSlug(p.package, agents) === slug);
}

/** SignInSummary is an agent card's badge: one state for all its methods. */
export interface SignInSummary {
  state: "off" | "signed_in" | "signed_out" | "probe_failed" | "none" | "loading";
  account?: string;
}

/** signInSummary is signed in when any method is, naming its account. */
export function signInSummary(
  enabled: boolean,
  methods: LoginMethodStatus[] | null | undefined,
  loading: boolean,
): SignInSummary {
  if (!enabled) return { state: "off" };
  if (loading) return { state: "loading" };
  const ms = methods ?? [];
  if (!ms.length) return { state: "none" };
  const signed = ms.find((m) => m.signed_in);
  if (signed) return { state: "signed_in", account: signed.account || undefined };
  if (ms.some((m) => m.error)) return { state: "probe_failed" };
  return { state: "signed_out" };
}

/** usageFacts are an agent card's usage lines. */
export function usageFacts(u: AgentUsage | undefined, now = Date.now()): [string, string][] {
  if (!u) return [];
  if (!u.tracked) return [["Usage", "Not tracked for this agent"]];
  const sessions = (n: number) => `${n} ${n === 1 ? "session" : "sessions"}`;
  return [
    ["Today", `${tokens(u.today.total)} tokens · ${sessions(u.today.sessions)}`],
    ["Last 7 days", `${tokens(u.week.total)} tokens · ${sessions(u.week.sessions)}`],
    ["Last active", lastActive(u.last_active, now)],
  ];
}

/** lastActive is "12 min ago", or "Not used in the last 7 days". */
export function lastActive(iso: string | null, now = Date.now()): string {
  return iso ? `${since(iso, now)} ago` : "Not used in the last 7 days";
}
