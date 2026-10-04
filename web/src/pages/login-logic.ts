import type { Job, LoginMethodStatus } from "@/api/types";

const MAX_CODE_BYTES = 2048;

/** cleanCode trims a pasted code; null when it is empty, too long or spans lines. */
export function cleanCode(raw: string): string | null {
  const code = raw.trim();
  if (!code || new TextEncoder().encode(code).length > MAX_CODE_BYTES) return null;
  for (const ch of code) {
    const n = ch.charCodeAt(0);
    if (n < 32 || n === 127) return null;
  }
  return code;
}

/** runningLogin is the sign-in in progress, from any tab, so its dialog can reopen. */
export function runningLogin(
  jobs: Job[] | undefined,
): { id: number; agent: string; method: string } | null {
  const j = (jobs ?? []).find((x) => x.verb === "agent.login" && x.state === "running");
  if (!j) return null;
  const args = j.args ?? [];
  return { id: j.id, agent: args[args.length - 2] ?? "", method: args[args.length - 1] ?? "" };
}

export type DialogStep =
  | { kind: "starting" }
  | { kind: "device"; url: string; code: string }
  | { kind: "paste"; url: string; waiting: boolean; retry: boolean }
  | { kind: "done"; account: string }
  | { kind: "failed"; message: string; badCode: boolean; cancelled: boolean };

/** dialogStep is what the sign-in dialog shows for a job. */
export function dialogStep(job: Job | undefined): DialogStep {
  const l = job?.login;
  if (job && job.state !== "running") {
    if (job.state === "succeeded" || l?.signed_in)
      return { kind: "done", account: l?.account ?? "" };
    return {
      kind: "failed",
      message: l?.failed || job.result?.error?.message || "The sign-in did not complete.",
      badCode: (l?.inputs ?? 0) > 0,
      cancelled: job.result?.error?.code === "cancelled",
    };
  }
  if (!l?.url) return { kind: "starting" };
  if (l.flow === "device") {
    return l.code ? { kind: "device", url: l.url, code: l.code } : { kind: "starting" };
  }
  // A new URL that waits again after a code means the CLI rejected it.
  return { kind: "paste", url: l.url, waiting: l.waiting, retry: l.inputs > 0 && l.waiting };
}

export interface MethodLine {
  id: string;
  label: string | null; // shown only when the agent has several methods
  flow: string;
  state: "signed_in" | "signed_out" | "unknown";
  account: string;
  error: string;
}

/** methodLines turns an agent's status into the Sign-in cell's lines. */
export function methodLines(methods: LoginMethodStatus[] | null | undefined): MethodLine[] {
  const list = methods ?? [];
  return list.map((m) => ({
    id: m.id,
    label: list.length > 1 ? m.label : null,
    flow: m.flow,
    state: m.error ? "unknown" : m.signed_in ? "signed_in" : "signed_out",
    account: m.signed_in ? m.account : "",
    error: m.error,
  }));
}
