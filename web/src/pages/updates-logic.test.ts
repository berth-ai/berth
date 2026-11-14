import { describe, expect, it } from "vitest";
import type { AgentsReport, Pin, UpdateCheck } from "@/api/types";
import {
  bannerText,
  checkSummary,
  newestFirst,
  pinRows,
  picked,
  updatable,
  updateAllBody,
} from "./updates-logic";

const pin = (over: Partial<Pin>): Pin => ({
  name: "gh",
  track: "tools",
  kind: "tool",
  title: "gh",
  image: "2.0.0",
  current: "2.0.0",
  ...over,
});

const check = (over: Partial<UpdateCheck>): UpdateCheck => ({
  checked_at: "",
  pins: [],
  updates: 0,
  majors: 0,
  update_available: false,
  ...over,
});

describe("updates", () => {
  it("merges the last check into the status pins of one track", () => {
    const agents = {
      pins: [
        pin({ name: "@openai/codex", track: "agents", kind: "agent", title: "Codex" }),
        pin({ name: "gh" }),
      ],
    } as AgentsReport;
    const c = check({ pins: [pin({ name: "gh", latest: "2.1.0", update: "minor" })] });
    expect(pinRows(agents, c, "tools")).toEqual([pin({ latest: "2.1.0", update: "minor" })]);
    expect(pinRows(agents, null, "agents").map((p) => p.title)).toEqual(["Codex"]);
    expect(pinRows(null, c, "tools")).toEqual([]);
  });

  it("updates all but majors", () => {
    const rows = [
      pin({ name: "a", update: "minor" }),
      pin({ name: "b", update: "major" }),
      pin({ name: "c" }),
      pin({ name: "d", update: "patch" }),
    ];
    expect(updatable(rows).map((p) => p.name)).toEqual(["a", "d"]);
    expect(picked(pin({ current: "2.1.0" }))).toBe(true);
    expect(picked(pin({}))).toBe(false);
  });

  it("summarises a check", () => {
    expect(checkSummary(check({ update_available: null, error: "offline" }))).toEqual({
      tone: "warn",
      text: "Could not check: offline",
    });
    expect(checkSummary(check({ update_available: true, updates: 2, majors: 1 })).text).toBe(
      "2 updates available, and 1 major update to pick one by one.",
    );
    expect(checkSummary(check({ update_available: true, majors: 2 })).text).toBe(
      "2 major updates to pick one by one.",
    );
    expect(checkSummary(check({ pins: [pin({ error: "no versions" })] })).text).toBe(
      "Everything else is current; 1 version could not be looked up.",
    );
    expect(checkSummary(check({}))).toEqual({
      tone: "ok",
      text: "Everything is at its newest version.",
    });
  });

  it("announces only minor and patch updates", () => {
    expect(bannerText(check({ update_available: true, updates: 1 }))).toBe(
      "1 newer version is available for your agents and tools.",
    );
    expect(bannerText(check({ update_available: true, majors: 3 }))).toBeNull();
    expect(bannerText(null)).toBeNull();
  });

  it("lists what Update all changes", () => {
    expect(updateAllBody([pin({ latest: "2.1.0" })])).toMatch(/^gh 2\.0\.0 → 2\.1\.0\. /);
  });

  it("orders versions newest first, pre-releases last", () => {
    expect(newestFirst(["1.0.0", "1.1.0-beta.1", "1.1.0", "2.0.0-rc.1"])).toEqual([
      "1.1.0",
      "1.0.0",
      "2.0.0-rc.1",
      "1.1.0-beta.1",
    ]);
  });
});
