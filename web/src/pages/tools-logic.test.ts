import { describe, expect, it } from "vitest";
import { isPrerelease, latestStable, parseToolsTab, presetPinned, upgradeTo } from "./tools-logic";

describe("parseToolsTab", () => {
  it("accepts the three tabs", () => {
    expect(parseToolsTab("builtin")).toBe("builtin");
    expect(parseToolsTab("yours")).toBe("yours");
    expect(parseToolsTab("presets")).toBe("presets");
  });

  it("falls back to builtin for anything else", () => {
    for (const v of [undefined, null, "", "YOURS", "../x", 3, ["yours"]]) {
      expect(parseToolsTab(v)).toBe("builtin");
    }
  });
});

describe("versions", () => {
  it("spots pre-releases", () => {
    for (const v of [
      "2.0.0-rc1",
      "3.0.0beta1",
      "1.2.0a1",
      "4.0.0.dev2",
      "1.0.0+build",
      "5.0-preview",
    ]) {
      expect(isPrerelease(v), v).toBe(true);
    }
    for (const v of ["1.20.0", "3.5a", "2025.4.7", "0.0.83", "21.0.8"]) {
      expect(isPrerelease(v), v).toBe(false);
    }
  });

  it("takes the last stable entry of an oldest-first list", () => {
    expect(latestStable(["1.0.0", "1.1.0", "2.0.0"])).toBe("2.0.0");
    expect(latestStable(["1.0.0", "1.1.0", "2.0.0-rc1"])).toBe("1.1.0");
    expect(latestStable(["2.0.0-rc1"])).toBeNull();
    expect(latestStable([])).toBeNull();
  });

  it("offers an upgrade only to a later stable version", () => {
    const list = ["1.0.0", "1.1.0", "2.0.0", "2.1.0-rc1"];
    expect(upgradeTo("1.1.0", list)).toBe("2.0.0");
    expect(upgradeTo("2.0.0", list)).toBeNull();
    expect(upgradeTo("2.1.0-rc1", list)).toBeNull();
  });

  it("offers nothing when the installed version is not listed or there is no list", () => {
    expect(upgradeTo("1.1", ["1.0.0", "1.1.0", "2.0.0"])).toBeNull();
    expect(upgradeTo("latest", ["1.0.0"])).toBeNull();
    expect(upgradeTo("1.0.0", undefined)).toBeNull();
    expect(upgradeTo("1.0.0", [])).toBeNull();
  });
});

describe("presetPinned", () => {
  it("prefers the tool named like the preset", () => {
    expect(presetPinned({ id: "python", tools: { uv: "0.12.20", python: "3.14.0" } })).toBe(
      "3.14.0",
    );
  });

  it("falls back to the first tool by name", () => {
    expect(presetPinned({ id: "web", tools: { pnpm: "10.0.0", node: "24.0.0" } })).toBe("24.0.0");
    expect(presetPinned({ id: "empty", tools: {} })).toBe("");
  });
});
