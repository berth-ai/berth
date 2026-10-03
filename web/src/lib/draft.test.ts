import { describe, expect, it } from "vitest";
import { diff, getPath, rebase, setPath } from "./draft";

describe("draft", () => {
  it("diffs only the listed paths", () => {
    const base = { a: { b: 1, c: 2 }, d: 3 };
    const draft = { a: { b: 9, c: 2 }, d: 4 };
    expect(diff(base, draft, ["a.b", "a.c"])).toEqual({ patch: { a: { b: 9 } }, changed: ["a.b"] });
  });

  it("sets nested paths, creating objects", () => {
    const o: Record<string, unknown> = {};
    setPath(o, "x.y.z", true);
    expect(getPath(o, "x.y.z")).toBe(true);
    expect(getPath(o, "x.q.z")).toBeUndefined();
  });

  it("keeps only this tab's edits after a conflict", () => {
    // Tab B edits the IDE toggle while tab A changed the time zone. After
    // the conflict, B's next save must carry its toggle and not revert A's
    // time zone.
    const old = { frontends: { ide: { enabled: false } }, runtime: { tz: "Etc/UTC" } };
    const draft = { frontends: { ide: { enabled: true } }, runtime: { tz: "Etc/UTC" } };
    const server = { frontends: { ide: { enabled: false } }, runtime: { tz: "Europe/Rome" } };
    const paths = ["frontends.ide.enabled", "runtime.tz"];
    const next = rebase(old, draft, server, paths);
    expect(diff(server, next, paths)).toEqual({
      patch: { frontends: { ide: { enabled: true } } },
      changed: ["frontends.ide.enabled"],
    });
    expect(server.frontends.ide.enabled).toBe(false);
  });
});
