import type { Changes, LockSource } from "@/api/types";

export function when(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

function span(seconds: number): string {
  if (seconds < 60) return `${Math.max(0, Math.floor(seconds))} s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} min`;
  if (seconds < 86400)
    return `${Math.floor(seconds / 3600)} h ${Math.floor((seconds % 3600) / 60)} min`;
  return `${Math.floor(seconds / 86400)} d ${Math.floor((seconds % 86400) / 3600)} h`;
}

/** since is the time elapsed since iso, as "3 h 12 min". */
export function since(iso: string | null | undefined, now = Date.now()): string {
  return iso ? span((now - new Date(iso).getTime()) / 1000) : "—";
}

export function duration(from: string | null | undefined, to: string | null | undefined): string {
  if (!from) return "—";
  const end = to ? new Date(to).getTime() : Date.now();
  return span((end - new Date(from).getTime()) / 1000);
}

export function short(hash: string | undefined): string {
  return hash ? hash.slice(0, 8) : "";
}

export function describeSource(s: LockSource | null | undefined): string {
  if (!s) return "unknown";
  if (s.origin === "image") return "image";
  if (s.origin === "ref")
    return `github:${s.ref || "?"}${s.commit ? ` (${s.commit.slice(0, 7)})` : ""}`;
  if (s.origin === "dir") return "local directory";
  return s.origin || "unknown";
}

export const SERVICE_LABELS: Record<string, string> = {
  panel: "Panel",
  ttyd: "Terminal",
  code_server: "IDE",
};

/** describeChanges says what a config change did to the services. */
export function describeChanges(ch: Changes | null | undefined): string {
  if (!ch) return "Saved.";
  const name = (n: string) => (SERVICE_LABELS[n] ?? n).toLowerCase();
  const parts: string[] = [];
  if (ch.started?.length) parts.push(`started the ${ch.started.map(name).join(" and the ")}`);
  if (ch.restarted?.length) parts.push(`restarted the ${ch.restarted.map(name).join(" and the ")}`);
  if (ch.stopped?.length) parts.push(`stopped the ${ch.stopped.map(name).join(" and the ")}`);
  if (!parts.length) return "Saved; nothing restarted.";
  return `Saved; ${parts.join("; ")}.`;
}

const UNITS = [
  [1e3, "k"],
  [1e6, "M"],
  [1e9, "B"],
] as const;

const round = (v: number) => (v >= 100 ? Math.round(v) : Number(v.toFixed(1)));

/** tokens shortens a token count: 950, 12.3k, 4.5M. */
export function tokens(n: number): string {
  if (n < 1000) return String(n);
  let i = n >= 1e9 ? 2 : n >= 1e6 ? 1 : 0;
  let v = round(n / UNITS[i][0]);
  // 999,999 rounds up to 1000k: say 1M.
  if (v >= 1000 && i < UNITS.length - 1) v = round(n / UNITS[++i][0]);
  return `${v}${UNITS[i][1]}`;
}

/** count words a number with its noun: "1 agent", "3 agents". */
export function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** dayLabel is a short weekday for a YYYY-MM-DD date: "Mon". */
export function dayLabel(date: string): string {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(y, m - 1, d).toLocaleDateString(undefined, { weekday: "short" });
}

/** dayTitle is a full date for a YYYY-MM-DD date: "Monday 26 October". */
export function dayTitle(date: string): string {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(y, m - 1, d).toLocaleDateString(undefined, {
    weekday: "long",
    day: "numeric",
    month: "long",
  });
}
