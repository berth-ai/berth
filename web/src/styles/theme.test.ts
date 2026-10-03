// @vitest-environment node
import { existsSync, readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const read = (rel: string) => readFileSync(new URL(rel, import.meta.url), "utf8");
const tokens = read("./tokens.css");
const theme = read("./theme.css");
const design = new URL("../../../internal/panel/DESIGN.md", import.meta.url);

// The Harbor colour tokens (DESIGN.md §2, §6), as M3's panel.css defined them.
const COLOURS = [
  "bg",
  "surface",
  "surface-sunken",
  "fg",
  "fg-secondary",
  "fg-muted",
  "line",
  "line-strong",
  "line-control",
  "accent",
  "accent-hover",
  "accent-soft",
  "accent-text",
  "accent-fg",
  "focus",
  "ok",
  "ok-soft",
  "warn",
  "warn-soft",
  "bad",
  "bad-soft",
  "bad-fg",
  "info",
  "info-soft",
  "idle",
  "idle-soft",
  "log-bg",
  "log-fg",
  "log-dim",
  "log-err",
  "thumb",
  "scrim",
  "shadow-1",
  "shadow-2",
];

function block(css: string, selector: string): string {
  const start = css.indexOf(selector);
  expect(start, selector).toBeGreaterThanOrEqual(0);
  const open = css.indexOf("{", start);
  return css.slice(open + 1, css.indexOf("}", open));
}

describe("Harbor tokens", () => {
  it("defines every colour token in the light set", () => {
    const light = block(tokens, ":root {");
    for (const name of COLOURS) expect(light, name).toContain(`--${name}:`);
  });

  it("keeps the two dark sets identical", () => {
    const media = block(tokens, ':root:not([data-theme="light"])');
    const forced = block(tokens, ':root[data-theme="dark"]');
    expect(media.replace(/\s+/g, " ").trim()).toBe(forced.replace(/\s+/g, " ").trim());
    expect(media).toContain("color-scheme: dark");
  });

  it.skipIf(!existsSync(design))("defines every token DESIGN.md names", () => {
    const text = readFileSync(design, "utf8");
    for (const [, name] of text.matchAll(/`(--[a-z0-9-]+)`/g)) {
      expect(tokens + theme, name).toContain(`${name}:`);
    }
  });

  it("restores the scrollbar rule Base UI no longer injects", () => {
    expect(theme).toContain(".base-ui-disable-scrollbar");
  });

  it("maps shadcn colours without redefining Harbor's --accent", () => {
    expect(theme).toContain("--color-primary: var(--accent);");
    expect(theme).not.toMatch(/^\s*--accent:/m);
  });
});
