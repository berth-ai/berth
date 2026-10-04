import type { AgentPackage, PackageKey, RegistryAgent, UpdateCheck } from "@/api/types";
import { short } from "@/lib/format";
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

export const addedToast = (name: string) => `${name} added. Sign in from its row.`;

export const removeBody = (p: Pick<AgentPackage, "skills_dir">) =>
  `Berth deletes its install. Its settings and sign-in in ${agentHome(p.skills_dir)} stay.`;
