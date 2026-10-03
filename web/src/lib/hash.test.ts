import { describe, expect, it } from "vitest";
import { legacyHash } from "./hash";

describe("legacyHash", () => {
  it.each([
    ["#overview", "#/"],
    ["#agents", "#/agents"],
    ["#settings", "#/settings"],
    ["#activity/12", "#/activity/12"],
  ])("maps %s to %s", (from, to) => expect(legacyHash(from)).toBe(to));

  it.each(["", "#", "#/", "#/agents", "#/activity/3"])("leaves %j alone", (hash) =>
    expect(legacyHash(hash)).toBeNull(),
  );
});
