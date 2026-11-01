import { describe, expect, it } from "vitest";
import {
  count,
  dayLabel,
  dayTitle,
  describeChanges,
  describeSource,
  duration,
  short,
  since,
  tokens,
} from "./format";

describe("format", () => {
  const now = Date.parse("2026-10-03T12:00:00Z");

  it("spans time like M3", () => {
    expect(since("2026-10-03T11:59:30Z", now)).toBe("30 s");
    expect(since("2026-10-03T11:45:00Z", now)).toBe("15 min");
    expect(since("2026-10-03T08:48:00Z", now)).toBe("3 h 12 min");
    expect(since("2026-10-01T09:00:00Z", now)).toBe("2 d 3 h");
    expect(since(null, now)).toBe("—");
    expect(duration("2026-10-03T11:00:00Z", "2026-10-03T11:00:42Z")).toBe("42 s");
  });

  it("shortens hashes and describes sources", () => {
    expect(short("3f9c2a7d1e0b4c65")).toBe("3f9c2a7d");
    expect(describeSource({ origin: "image", fetched_at: "" })).toBe("image");
    expect(
      describeSource({ origin: "ref", ref: "main", commit: "a1b2c3d4e5f6", fetched_at: "" }),
    ).toBe("github:main (a1b2c3d)");
    expect(describeSource({ origin: "dir", fetched_at: "" })).toBe("local directory");
    expect(describeSource(null)).toBe("unknown");
  });

  it("says what a config change did", () => {
    expect(describeChanges(null)).toBe("Saved.");
    expect(describeChanges({ started: [], restarted: [], stopped: [] })).toBe(
      "Saved; nothing restarted.",
    );
    expect(describeChanges({ started: ["code_server"], restarted: ["ttyd"], stopped: null })).toBe(
      "Saved; started the ide; restarted the terminal.",
    );
  });
});

describe("count", () => {
  it("picks the singular only for one", () => {
    expect(count(0, "agent", "agents")).toBe("0 agents");
    expect(count(1, "agent", "agents")).toBe("1 agent");
    expect(count(12, "tool", "tools")).toBe("12 tools");
  });

  it("shortens token counts", () => {
    expect(tokens(0)).toBe("0");
    expect(tokens(950)).toBe("950");
    expect(tokens(1000)).toBe("1k");
    expect(tokens(12_345)).toBe("12.3k");
    expect(tokens(999_999)).toBe("1M");
    expect(tokens(4_500_000)).toBe("4.5M");
    expect(tokens(123_456_789)).toBe("123M");
    expect(tokens(2_000_000_000)).toBe("2B");
  });

  it("labels days by their calendar date, not UTC", () => {
    expect(dayLabel("2026-10-26")).toBe(
      new Date(2026, 9, 26).toLocaleDateString(undefined, { weekday: "short" }),
    );
    expect(dayTitle("2026-11-01")).toBe(
      new Date(2026, 10, 1).toLocaleDateString(undefined, {
        weekday: "long",
        day: "numeric",
        month: "long",
      }),
    );
  });
});
