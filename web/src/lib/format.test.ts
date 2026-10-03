import { describe, expect, it } from "vitest";
import { describeChanges, describeSource, duration, short, since } from "./format";

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
