import { describe, expect, it } from "vitest";
import type { AgentPackage, LoginStatus, ServiceStatus } from "@/api/types";
import { healthRows, healthSummary, setupSteps } from "./overview-logic";

const agent = (over: Partial<AgentPackage>): AgentPackage =>
  ({
    package: "@x/cc",
    name: "CC",
    kind: "agent",
    enabled: true,
    keys: [],
    ...over,
  }) as AgentPackage;

const logins = (signed: boolean): LoginStatus => ({
  agents: {
    "@x/cc": {
      name: "CC",
      methods: [{ id: "a", label: "A", flow: "paste", signed_in: signed, account: "", error: "" }],
    },
  },
});

const svc = (over: Partial<ServiceStatus>): ServiceStatus => ({
  name: "ttyd",
  state: "running",
  pid: 1,
  started_at: "2026-11-15T10:00:00Z",
  restarts: 0,
  log: null,
  ...over,
});

describe("setupSteps", () => {
  const git = { name: "", email: "" };

  it("waits until everything has loaded", () => {
    expect(setupSteps({ packages: [], logins: undefined, git, workspaces: 0 })).toBeNull();
  });

  it("marks each step from what Berth knows", () => {
    const steps = setupSteps({
      packages: [agent({})],
      logins: logins(false),
      git: { name: "A", email: "" },
      workspaces: 1,
    });
    expect(steps?.map((s) => [s.id, s.done])).toEqual([
      ["agent", false],
      ["git", false],
      ["workspace", true],
    ]);
  });

  it("counts a sign-in or a saved key, but not an agent that is off", () => {
    const done = (packages: AgentPackage[], l: LoginStatus) =>
      setupSteps({ packages, logins: l, git, workspaces: 0 })?.[0].done;
    expect(done([agent({})], logins(true))).toBe(true);
    expect(done([agent({ keys: [{ name: "K", set: true }] })], logins(false))).toBe(true);
    expect(done([agent({ enabled: false, keys: [{ name: "K", set: true }] })], logins(true))).toBe(
      false,
    );
  });
});

describe("health", () => {
  const now = Date.parse("2026-11-15T10:30:00Z");

  it("leaves the panel out unless it restarted lately or is in trouble", () => {
    const rows = (panel: Partial<ServiceStatus>) =>
      healthRows([svc({}), svc({ name: "panel", ...panel })], now).map((s) => s.name);
    expect(rows({})).toEqual(["ttyd"]);
    expect(rows({ restarts: 1 })).toEqual(["ttyd", "panel"]);
    expect(rows({ restarts: 1, started_at: "2026-11-14T10:00:00Z" })).toEqual(["ttyd"]);
    expect(rows({ state: "failed" })).toEqual(["ttyd", "panel"]);
  });

  it("sums the services in one line", () => {
    expect(healthSummary([svc({})]).state).toBe("running");
    expect(healthSummary([svc({ state: "restarting" })]).state).toBe("restarting");
    expect(healthSummary([svc({ state: "restarting" }), svc({ state: "failed" })]).state).toBe(
      "failed",
    );
  });
});
