import type { AgentVersion, AgentsReport, Pin, Track, UpdateCheck } from "@/api/types";
import type { Tone } from "@/lib/notify";

/** pinRows are the pins of one track as the status reports them, each with
 * the newest version and the kind of update from the last check. */
export function pinRows(
  agents: AgentsReport | null | undefined,
  check: UpdateCheck | null | undefined,
  track: Track,
): Pin[] {
  const checked = new Map((check?.pins ?? []).map((p) => [p.name, p]));
  return (agents?.pins ?? [])
    .filter((p) => p.track === track)
    .map((p) => {
      const c = checked.get(p.name);
      return c ? { ...p, latest: c.latest, update: c.update, error: c.error } : p;
    });
}

/** updatable lists the pins Update all moves: minor and patch updates.
 * Majors wait until they are picked one by one. */
export function updatable(pins: Pin[]): Pin[] {
  return pins.filter((p) => p.update && p.update !== "major");
}

/** picked reports whether a pin runs another version than the image's. */
export const picked = (p: Pin) => p.current !== p.image;

export const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** checkSummary says what the last check found. */
export function checkSummary(c: UpdateCheck): { tone: Tone; text: string } {
  if (c.update_available === null) return { tone: "warn", text: `Could not check: ${c.error}` };
  const majors = c.majors
    ? `${plural(c.majors, "major update", "major updates")} to pick one by one`
    : "";
  if (c.updates)
    return {
      tone: "warn",
      text: `${plural(c.updates, "update", "updates")} available${majors ? `, and ${majors}` : ""}.`,
    };
  if (c.majors) return { tone: "warn", text: `${majors[0].toUpperCase()}${majors.slice(1)}.` };
  const failed = (c.pins ?? []).filter((p) => p.error).length;
  if (failed)
    return {
      tone: "warn",
      text: `Everything else is current; ${plural(failed, "version", "versions")} could not be looked up.`,
    };
  return { tone: "ok", text: "Everything is at its newest version." };
}

/** bannerUpdates are the minor and patch updates the banner names, agents
 * and MCP servers before base tools. */
export function bannerUpdates(c: UpdateCheck | null | undefined): Pin[] {
  if (!c?.update_available || !c.updates) return [];
  return updatable(c.pins ?? []).sort(
    (a, b) => Number(a.track !== "agents") - Number(b.track !== "agents"),
  );
}

const move = (p: Pin) => `${p.title} ${p.current} → ${p.latest}`;

/** bannerText announces minor and patch updates by name, the first two in
 * full; null when there are none. */
export function bannerText(c: UpdateCheck | null | undefined): string | null {
  if (!c?.update_available || !c.updates) return null;
  const pins = bannerUpdates(c);
  if (!pins.length)
    return `${plural(c.updates, "newer version is", "newer versions are")} available for your agents and tools.`;
  if (pins.length === 1) return `${move(pins[0])} is available.`;
  if (pins.length === 2) return `${move(pins[0])} and ${move(pins[1])} are available.`;
  return `${move(pins[0])}, ${move(pins[1])} and ${pins.length - 2} more are available.`;
}

/** bannerKey names what the banner announces, so dismissing it lasts
 * until a check finds a version not yet dismissed. */
export function bannerKey(c: UpdateCheck | null | undefined): string {
  return bannerUpdates(c)
    .map((p) => `${p.name}@${p.latest}`)
    .sort()
    .join(" ");
}

/** dismissedCovers reports whether a dismissed key still covers the
 * current one: every announced version was announced when dismissed. */
export function dismissedCovers(dismissed: string, key: string): boolean {
  if (!dismissed || !key) return false;
  const seen = new Set(dismissed.split(" "));
  return key.split(" ").every((k) => seen.has(k));
}

/** setChanges says what an installed set changed from the one before it,
 * by title where the image pins the package; "" when nothing differs. */
export function setChanges(
  v: AgentVersion,
  before: AgentVersion | undefined,
  titles: Map<string, string>,
): string {
  const now = v.packages ?? {};
  if (!before) return "First set";
  const was = before.packages ?? {};
  const name = (n: string) => titles.get(n) ?? n;
  const moves: string[] = [];
  for (const n of Object.keys(now).sort((a, b) => name(a).localeCompare(name(b)))) {
    if (!(n in was)) moves.push(`${name(n)} ${now[n]} added`);
    else if (was[n] !== now[n]) moves.push(`${name(n)} ${was[n]} → ${now[n]}`);
  }
  for (const n of Object.keys(was).sort()) if (!(n in now)) moves.push(`${name(n)} removed`);
  if (moves.length > 2) return `${moves.slice(0, 2).join(", ")} and ${moves.length - 2} more`;
  return moves.join(", ");
}

/** setBefore is the set installed just before v, if the list has it. */
export function setBefore(v: AgentVersion, all: AgentVersion[]): AgentVersion | undefined {
  let best: AgentVersion | undefined;
  for (const o of all)
    if (
      o.hash !== v.hash &&
      o.installed_at < v.installed_at &&
      (!best || o.installed_at > best.installed_at)
    )
      best = o;
  return best;
}

/** updateAllBody lists what Update all changes. */
export function updateAllBody(pins: Pin[]): string {
  const names = pins.map((p) => `${p.title} ${p.current} → ${p.latest}`);
  return `${names.join(", ")}. Each installs beside the current set and switches atomically; running sessions keep their version.`;
}

/** newestFirst orders versions for a picker: stable ones first, newest
 * first, then pre-releases. */
export function newestFirst(versions: string[]): string[] {
  const stable = versions.filter((v) => !v.includes("-"));
  const pre = versions.filter((v) => v.includes("-"));
  return [...stable.reverse(), ...pre.reverse()];
}
