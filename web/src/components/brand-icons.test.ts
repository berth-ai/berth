// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { BRAND_ICONS, contrast, iconFill, monogram, SURFACES } from "./brand-icons";

describe("brand icons", () => {
  it("knows the lock's slugs", () => {
    expect(Object.keys(BRAND_ICONS).sort()).toEqual([
      "claude",
      "googlegemini",
      "opencode",
      "upstash",
    ]);
  });

  it("checks contrast against the theme's surfaces", () => {
    const tokens = readFileSync(new URL("../styles/tokens.css", import.meta.url), "utf8");
    const surfaces = [...tokens.matchAll(/--surface:\s*#([0-9a-f]{6});/g)].map((m) => m[1]);
    expect(new Set(surfaces)).toEqual(new Set(SURFACES));
  });

  it("measures WCAG contrast", () => {
    expect(contrast("#000000", "#ffffff")).toBeCloseTo(21, 0);
    expect(contrast("#ffffff", "#ffffff")).toBeCloseTo(1, 5);
  });

  it("keeps a brand colour only when it reads on both surfaces", () => {
    expect(iconFill("8E75B2")).toBe("#8E75B2"); // Gemini: 3.93:1 and 4.37:1
    expect(iconFill("D97757")).toBe("#D97757"); // Claude: 3.12:1 on white, 5.5:1 on dark
    expect(iconFill("000000")).toBe("currentColor"); // OpenCode: 1.22:1 on dark
    expect(iconFill("00E9A3")).toBe("currentColor"); // Upstash: 1.59:1 on white
  });

  it("makes stable monograms", () => {
    expect(monogram("Codex", "@openai/codex")).toEqual(monogram("Codex", "@openai/codex"));
    expect(monogram("codex", "x").letter).toBe("C");
    expect(monogram("  @remote-demo", "remote-demo").letter).toBe("R");
    expect(monogram("", "x").letter).toBe("?");
  });
});
