import type { AgentsReport, Pin, Track, UpdateCheck } from "@/api/types";
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

/** bannerText announces minor and patch updates; null when there are none. */
export function bannerText(c: UpdateCheck | null | undefined): string | null {
  if (!c?.update_available || !c.updates) return null;
  return `${plural(c.updates, "newer version is", "newer versions are")} available for your agents and tools.`;
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
