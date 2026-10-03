import type { AgentPackage, PackageKey, UpdateCheck } from "@/api/types";
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
