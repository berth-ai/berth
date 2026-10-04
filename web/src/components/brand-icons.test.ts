// @vitest-environment node
import { describe, expect, it } from "vitest";
import { BRAND_ICONS, monogram } from "./brand-icons";

describe("brand icons", () => {
  it("knows the lock's slugs", () => {
    expect(Object.keys(BRAND_ICONS).sort()).toEqual([
      "claude",
      "codex",
      "googlegemini",
      "opencode",
    ]);
  });

  it("makes stable monograms", () => {
    expect(monogram("Codex", "@openai/codex")).toEqual(monogram("Codex", "@openai/codex"));
    expect(monogram("codex", "x").letter).toBe("C");
    expect(monogram("  @remote-demo", "remote-demo").letter).toBe("R");
    expect(monogram("", "x").letter).toBe("?");
  });
});
