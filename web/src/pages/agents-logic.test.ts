import { describe, expect, it } from "vitest";
import type { AgentPackage, AgentUsage, LoginMethodStatus, RegistryAgent } from "@/api/types";
import {
  addedToast,
  agentHome,
  agentSlug,
  allAdded,
  checkResult,
  disabledAfter,
  findAgent,
  keyBadge,
  lastActive,
  removeBody,
  signInSummary,
  usageFacts,
  versionLabel,
} from "./agents-logic";

const pkg = (name: string, enabled: boolean): AgentPackage => ({
  package: name,
  name,
  kind: "agent",
  version: "1",
  enabled,
  bins: [],
  keys: [],
});

describe("agents logic", () => {
  it("words an update check like M3", () => {
    expect(
      checkResult({ ref: "main", update_available: null, error: "timeout", checked_at: "" }),
    ).toEqual({ tone: "warn", text: "Could not reach main: timeout" });
    expect(
      checkResult({ ref: "main", hash: "9e8d7c6b5a4f", update_available: true, checked_at: "" }),
    ).toEqual({ tone: "warn", text: "Update available on main: 9e8d7c6b." });
    expect(checkResult({ ref: "v2", update_available: false, checked_at: "" })).toEqual({
      tone: "ok",
      text: "Up to date with v2.",
    });
  });

  it("computes the disabled list after one toggle", () => {
    const all = [pkg("a", true), pkg("b", false), pkg("c", true)];
    expect(disabledAfter(all, "a", false)).toEqual(["b", "a"]);
    expect(disabledAfter(all, "b", true)).toEqual([]);
  });

  it("labels history entries", () => {
    expect(versionLabel("h1", "h1", "h2")).toBe("Current");
    expect(versionLabel("h2", "h1", "h2")).toBe("Previous");
    expect(versionLabel("h3", "h1", "h2")).toBe("Kept");
  });
});

describe("keyBadge", () => {
  it("maps a key onto a badge state", () => {
    expect(keyBadge({ name: "A", set: true })).toBe("set");
    expect(keyBadge({ name: "A", set: false })).toBe("not_set");
  });
});

describe("added agents", () => {
  const entry = (added: boolean): RegistryAgent => ({
    id: "copilot",
    package: "@github/copilot",
    name: "GitHub Copilot CLI",
    description: "",
    version: "1.0.91",
    keys: [],
    login: [],
    added,
  });

  it("hides Add agent only when every entry is added", () => {
    expect(allAdded(undefined)).toBe(false);
    expect(allAdded([entry(false)])).toBe(false);
    expect(allAdded([entry(true)])).toBe(true);
    expect(allAdded([])).toBe(true);
  });

  it("names the folder an agent keeps its settings in", () => {
    expect(agentHome("~/.copilot/skills")).toBe("~/.copilot");
    expect(agentHome("$XDG_CONFIG_HOME/opencode/skills")).toBe("$XDG_CONFIG_HOME/opencode");
    expect(agentHome(undefined)).toBe("its own folder");
  });

  it("words the toast and the remove confirmation", () => {
    expect(addedToast("GitHub Copilot CLI")).toBe(
      "GitHub Copilot CLI added. Sign in from its page.",
    );
    expect(removeBody({ skills_dir: "~/.copilot/skills" })).toBe(
      "Berth deletes its install. Its settings and sign-in in ~/.copilot stay.",
    );
  });

  it("names agents in URLs without their scope, unless two share a name", () => {
    const all = [
      pkg("@anthropic-ai/claude-code", true),
      pkg("opencode-ai", true),
      pkg("@a/cli", true),
      pkg("@b/cli", true),
      { ...pkg("@x/claude-code-mcp", true), kind: "mcp" },
    ];
    expect(agentSlug("@anthropic-ai/claude-code", all)).toBe("claude-code");
    expect(agentSlug("opencode-ai", all)).toBe("opencode-ai");
    expect(agentSlug("@a/cli", all)).toBe("a~cli");
    expect(findAgent(all, "claude-code")?.package).toBe("@anthropic-ai/claude-code");
    expect(findAgent(all, "b~cli")?.package).toBe("@b/cli");
    expect(findAgent(all, "cli")).toBeUndefined();
    expect(findAgent(all, "claude-code-mcp")).toBeUndefined();
  });

  const method = (m: Partial<LoginMethodStatus>): LoginMethodStatus => ({
    id: "m",
    label: "M",
    flow: "device",
    signed_in: false,
    account: "",
    error: "",
    ...m,
  });

  it("sums an agent's sign-ins for its card", () => {
    expect(signInSummary(false, [method({ signed_in: true })], false)).toEqual({ state: "off" });
    expect(signInSummary(true, undefined, true)).toEqual({ state: "loading" });
    expect(signInSummary(true, [], false)).toEqual({ state: "none" });
    expect(
      signInSummary(
        true,
        [method({}), method({ signed_in: true, account: "you@example.org" })],
        false,
      ),
    ).toEqual({ state: "signed_in", account: "you@example.org" });
    expect(signInSummary(true, [method({ signed_in: true })], false)).toEqual({
      state: "signed_in",
      account: undefined,
    });
    expect(signInSummary(true, [method({}), method({ error: "timeout" })], false)).toEqual({
      state: "probe_failed",
    });
    expect(signInSummary(true, [method({})], false)).toEqual({ state: "signed_out" });
  });

  const period = (total: number, sessions: number) => ({
    input: 0,
    output: 0,
    cache_read: 0,
    cache_write: 0,
    total,
    messages: 0,
    sessions,
  });
  const usage = (u: Partial<AgentUsage>): AgentUsage => ({
    name: "A",
    tracked: true,
    today: period(0, 0),
    week: period(0, 0),
    days: [],
    models: [],
    last_active: null,
    error: "",
    ...u,
  });

  it("words an agent's usage for its card", () => {
    const now = Date.parse("2026-11-01T15:00:00Z");
    expect(usageFacts(undefined)).toEqual([]);
    expect(usageFacts(usage({ tracked: false }))).toEqual([
      ["Usage", "Not tracked for this agent"],
    ]);
    expect(
      usageFacts(
        usage({
          today: period(1_368_850, 1),
          week: period(8_347_775, 14),
          last_active: "2026-11-01T14:48:00Z",
        }),
        now,
      ),
    ).toEqual([
      ["Today", "1.4M tokens · 1 session"],
      ["Last 7 days", "8.3M tokens · 14 sessions"],
      ["Last active", "12 min ago"],
    ]);
    expect(lastActive(null)).toBe("Not used in the last 7 days");
  });
});
