// @vitest-environment node
import { describe, expect, it } from "vitest";
import { BRAND_ICONS, monogram } from "./brand-icons";

describe("brand icons", () => {
  it("knows the lock's slugs", () => {
    expect(Object.keys(BRAND_ICONS).sort()).toEqual([
      "claude",
      "codex",
      "githubcopilot",
      "googlegemini",
      "opencode",
    ]);
  });

  it("fills only from colours or the mark's own gradients", () => {
    for (const [slug, mark] of Object.entries(BRAND_ICONS)) {
      for (const p of mark.paths) {
        if (typeof p.fill === "string") {
          expect(p.fill, slug).toMatch(/^(#[0-9A-Fa-f]{3,6}|currentColor)$/);
        } else {
          expect(mark.gradients?.[p.fill.gradient], slug).toBeDefined();
        }
      }
    }
  });

  it("makes stable monograms", () => {
    expect(monogram("Codex", "@openai/codex")).toEqual(monogram("Codex", "@openai/codex"));
    expect(monogram("codex", "x").letter).toBe("C");
    expect(monogram("  @remote-demo", "remote-demo").letter).toBe("R");
    expect(monogram("", "x").letter).toBe("?");
  });
});
