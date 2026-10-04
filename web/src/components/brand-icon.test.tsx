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
    expect(container.querySelector("svg[aria-hidden='true'] path")).not.toBeNull();
    expect(container.textContent).toBe("");
  });

  it("keeps a monogram for an unknown agent", () => {
    const { container } = render(<BrandIcon id="@example/agent" name="Example Agent" />);
    expect(container.querySelector("svg")).toBeNull();
    expect(container.textContent).toBe("E");
  });
});
