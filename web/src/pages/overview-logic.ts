import type { AgentPackage, LoginStatus, ServiceStatus } from "@/api/types";
import { signInSummary } from "./agents-logic";

/** SetupStep is one item of the getting-started checklist. */
export interface SetupStep {
  id: "agent" | "git" | "workspace";
  label: string;
  hint: string;
  done: boolean;
  to: "/agents" | "/accounts" | "/workspaces";
  action: string;
}

/** setupSteps is the checklist the Overview keeps until every step is
 * done: an agent that can run, a git identity for its commits, and a
 * first workspace. Null until everything it reads has loaded. */
export function setupSteps(input: {
  packages: AgentPackage[] | undefined;
  logins: LoginStatus | undefined;
  git: { name: string; email: string } | undefined;
  workspaces: number | undefined;
}): SetupStep[] | null {
  const { packages, logins, git, workspaces } = input;
  if (!packages || !logins || !git || workspaces === undefined) return null;
  const agentReady = packages.some((p) => {
    if (p.kind !== "agent") return false;
    const s = signInSummary(p.enabled, logins.agents?.[p.package]?.methods, false);
    // A saved API key counts as much as a sign-in.
    return s.state === "signed_in" || (p.enabled && (p.keys ?? []).some((k) => k.set));
  });
  return [
    {
      id: "agent",
      label: "Sign in to an agent",
      hint: "Use your subscription, or save an API key.",
      done: agentReady,
      to: "/agents",
      action: "Open Agents",
    },
    {
      id: "git",
      label: "Set your git identity",
      hint: "Without it, agent commits have no author.",
      done: !!git.name && !!git.email,
      to: "/accounts",
      action: "Open Accounts",
    },
    {
      id: "workspace",
      label: "Add your first workspace",
      hint: "Clone a repository or adopt a folder in /workspaces.",
      done: workspaces > 0,
      to: "/workspaces",
      action: "Open Workspaces",
    },
  ];
}

/** RECENT is how long after a restart the panel's own row still shows. */
const RECENT = 60 * 60 * 1000;

/** troubled reports whether a service needs the operator: failed or
 * restarting. */
export const troubled = (s: ServiceStatus) => s.state === "failed" || s.state === "restarting";

/** healthRows are the services the Overview's health card lists. The panel
 * is left out while it is fine: the user is looking at it. It shows when
 * it is in trouble or restarted in the last hour. */
export function healthRows(services: ServiceStatus[], now = Date.now()): ServiceStatus[] {
  return services.filter((s) => {
    if (s.name !== "panel") return true;
    if (troubled(s)) return true;
    const started = s.started_at ? Date.parse(s.started_at) : NaN;
    return s.restarts > 0 && now - started < RECENT;
  });
}

/** healthSummary is the health card's one line. */
export function healthSummary(services: ServiceStatus[]): { state: string; text: string } {
  const bad = services.filter(troubled);
  if (bad.some((s) => s.state === "failed"))
    return { state: "failed", text: "A service has failed." };
  if (bad.length) return { state: "restarting", text: "A service is restarting." };
  return { state: "running", text: "Everything is running." };
}
