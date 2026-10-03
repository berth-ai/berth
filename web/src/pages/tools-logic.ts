import type { Preset } from "@/api/types";

export const TOOLS_TABS = ["builtin", "yours", "presets"] as const;
export type ToolsTab = (typeof TOOLS_TABS)[number];

/** parseToolsTab reads the ?tab= search value; anything unknown is Built-in. */
export function parseToolsTab(v: unknown): ToolsTab {
  return TOOLS_TABS.find((t) => t === v) ?? "builtin";
}

// A dash or plus suffix, or a pre-release word or Python-style tag
// (1.2.0a1, 3.0.0beta1, 4.0.0.dev2). "3.5a" (tmux) has no digit after the
// letter, so it stays stable.
const PRERELEASE = /[-+]|alpha|beta|rc\d*$|\d(a|b)\d+$|dev\d*$|preview|snapshot|nightly/i;

export function isPrerelease(v: string): boolean {
  return PRERELEASE.test(v);
}

/** latestStable is the newest stable version of an oldest-first list
 * (`tools.versions` is `mise ls-remote` output). */
export function latestStable(versions: readonly string[]): string | null {
  for (let i = versions.length - 1; i >= 0; i--) {
    if (!isPrerelease(versions[i])) return versions[i];
  }
  return null;
}

/** upgradeTo is the version an installed tool can move up to, or null. The
 * installed version must itself be listed, so "1.20" against "1.20.0" is
 * never offered an upgrade. */
export function upgradeTo(
  installed: string,
  versions: readonly string[] | undefined,
): string | null {
  if (!versions) return null;
  const at = versions.indexOf(installed);
  const latest = latestStable(versions);
  if (at < 0 || latest === null) return null;
  return versions.indexOf(latest) > at ? latest : null;
}

/** presetPinned is the version shown for a preset: its namesake tool's,
 * else the first tool's by name. */
export function presetPinned(p: Pick<Preset, "id" | "tools">): string {
  if (Object.hasOwn(p.tools, p.id)) return p.tools[p.id];
  const first = Object.keys(p.tools).sort()[0];
  return first === undefined ? "" : p.tools[first];
}
