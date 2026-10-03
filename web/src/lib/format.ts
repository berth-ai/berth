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

/** count words a number with its noun: "1 agent", "3 agents". */
export function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}
