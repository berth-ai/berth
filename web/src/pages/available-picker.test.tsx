import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentPackage } from "@/api/types";
import { AvailablePicker } from "./available-picker";

const agent: AgentPackage = {
  package: "a",
  name: "Alpha",
  kind: "agent",
  version: "1",
  enabled: true,
  bins: [],
  keys: [],
};

describe("AvailablePicker", () => {
  it("adds a server to a workspace", async () => {
    const onChange = vi.fn(async () => {});
    render(
      <AvailablePicker
        server="files"
        value={undefined}
        agents={[agent]}
        onChange={vi.fn(async () => {})}
        workspaces={{
          all: ["api", "web"],
          value: ["web"],
          note: "Codex will trust each workspace you tick.",
          onChange,
        }}
      />,
    );
    const trigger = screen.getByRole("button", { name: "Choose agents for files" });
    expect(trigger.textContent).toBe("All agents · web");
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByRole("checkbox", { name: /^api/ }));
    await vi.waitFor(() => expect(onChange).toHaveBeenCalledWith(["api", "web"]));
    expect(screen.getByText("Codex will trust each workspace you tick.")).toBeTruthy();
  });
});
