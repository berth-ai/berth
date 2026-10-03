import { describe, expect, it } from "vitest";
import type { AgentPackage } from "@/api/types";
import { checkResult, disabledAfter, keyBadge, versionLabel } from "./agents-logic";

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
