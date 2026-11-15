import { describe, expect, it } from "vitest";
import type { AgentVersion, AgentsReport, Pin, UpdateCheck } from "@/api/types";
import {
  bannerKey,
  bannerText,
  dismissedCovers,
  setBefore,
  setChanges,
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
    const cc = pin({
      name: "@anthropic-ai/claude-code",
      track: "agents",
      title: "Claude Code",
      current: "2.1.321",
      latest: "2.1.330",
      update: "patch",
    });
    const gh = pin({ latest: "2.1.0", update: "minor" });
    const node = pin({
      name: "node",
      title: "node",
      current: "24.1.0",
      latest: "24.2.0",
      update: "minor",
    });
    const big = pin({ name: "rg", title: "rg", latest: "3.0.0", update: "major" });
    expect(bannerText(check({ update_available: true, updates: 1, pins: [big, gh] }))).toBe(
      "gh 2.0.0 → 2.1.0 is available.",
    );
    expect(bannerText(check({ update_available: true, updates: 2, pins: [gh, cc] }))).toBe(
      "Claude Code 2.1.321 → 2.1.330 and gh 2.0.0 → 2.1.0 are available.",
    );
    expect(bannerText(check({ update_available: true, updates: 3, pins: [gh, node, cc] }))).toBe(
      "Claude Code 2.1.321 → 2.1.330, gh 2.0.0 → 2.1.0 and 1 more are available.",
    );
    expect(bannerText(check({ update_available: true, majors: 3 }))).toBeNull();
    expect(bannerText(null)).toBeNull();
  });

  it("keeps a dismissal until a new version is announced", () => {
    const gh = pin({ latest: "2.1.0", update: "minor" });
    const node = pin({ name: "node", latest: "24.2.0", update: "patch" });
    const one = bannerKey(check({ update_available: true, updates: 1, pins: [gh] }));
    const two = bannerKey(check({ update_available: true, updates: 2, pins: [gh, node] }));
    expect(one).toBe("gh@2.1.0");
    expect(dismissedCovers(two, one)).toBe(true);
    expect(dismissedCovers(one, two)).toBe(false);
    expect(dismissedCovers("", one)).toBe(false);
    expect(
      dismissedCovers(
        one,
        bannerKey(
          check({ update_available: true, updates: 1, pins: [{ ...gh, latest: "2.2.0" }] }),
        ),
      ),
    ).toBe(false);
  });

  it("says what each set changed", () => {
    const set = (hash: string, at: string, packages: Record<string, string>): AgentVersion => ({
      hash,
      source: { origin: "image", fetched_at: at },
      packages,
      installed_at: at,
    });
    const a = set("a", "2026-11-01T00:00:00Z", { "@x/cc": "1.0.0", gh: "2.0.0" });
    const b = set("b", "2026-11-02T00:00:00Z", { "@x/cc": "1.1.0", gh: "2.0.0", rg: "15.0.0" });
    const c = set("c", "2026-11-03T00:00:00Z", { "@x/cc": "1.1.0" });
    const titles = new Map([["@x/cc", "Claude Code"]]);
    const all = [c, a, b];
    expect(setBefore(b, all)).toBe(a);
    expect(setBefore(a, all)).toBeUndefined();
    expect(setChanges(a, undefined, titles)).toBe("First set");
    expect(setChanges(b, a, titles)).toBe("Claude Code 1.0.0 → 1.1.0, rg 15.0.0 added");
    expect(setChanges(c, b, titles)).toBe("gh removed, rg removed");
    expect(setChanges(c, c, titles)).toBe("");
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
