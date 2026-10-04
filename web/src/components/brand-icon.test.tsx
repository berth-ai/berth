import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { BrandIcon } from "./brand-icon";

describe("BrandIcon", () => {
  it.each([
    ["@anthropic-ai/claude-code", "Claude Code"],
    ["@google/gemini-cli", "Gemini CLI"],
    ["@openai/codex", "Codex"],
    ["opencode-ai", "OpenCode"],
  ])("shows a logo for %s without installed icon metadata", (id, name) => {
    const { container } = render(<BrandIcon id={id} name={name} />);
    expect(container.querySelector("[aria-hidden='true'] svg path")).not.toBeNull();
    expect(container.textContent).toBe("");
  });

  it("draws a known mark at 20 px inside a 28 px frame", () => {
    const { container } = render(<BrandIcon id="@openai/codex" name="Codex" />);
    const frame = container.firstElementChild;
    expect(frame?.tagName).toBe("SPAN");
    expect(frame?.getAttribute("aria-hidden")).toBe("true");
    expect(frame?.classList).toContain("size-7");
    expect(frame?.querySelector("svg")?.classList).toContain("size-5");
  });

  it("keeps a monogram for an unknown agent", () => {
    const { container } = render(<BrandIcon id="@example/agent" name="Example Agent" />);
    expect(container.querySelector("svg")).toBeNull();
    expect(container.textContent).toBe("E");
    expect(container.firstElementChild?.classList).toContain("size-7");
  });
});
