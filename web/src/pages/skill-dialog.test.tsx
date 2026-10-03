import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentPackage } from "@/api/types";
import { AgentChecklist } from "./available-picker";
import { SkillDialog } from "./skill-dialog";

vi.mock("@/api/run-job", () => ({ runJob: vi.fn() }));

const agent = (pkg: string, name: string): AgentPackage => ({
  package: pkg,
  name,
  kind: "agent",
  version: "1",
  enabled: true,
  bins: [],
  keys: [],
});

const type = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } });

describe("SkillDialog", () => {
  it("validates the repository form before starting a job", async () => {
    const { runJob } = await import("@/api/run-job");
    render(
      <SkillDialog agents={[]} unsupported={[]} installed={[]} editing={null} onClose={() => {}} />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "From a repository" }));
    type("Repository", "http://example.com/x");
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));
    expect(await screen.findByText("Start the URL with https://.")).toBeTruthy();
    expect(runJob).not.toHaveBeenCalled();
  });

  it("greys out catalog skills that are installed", async () => {
    render(
      <SkillDialog
        agents={[]}
        unsupported={[]}
        installed={["pdf"]}
        editing={null}
        onClose={() => {}}
      />,
    );
    expect(
      (await screen.findByRole<HTMLButtonElement>("button", { name: /Add pdf/ })).disabled,
    ).toBe(true);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: /Add docx/ }).disabled).toBe(
      false,
    );
  });

  it("shows a job's field error on that field", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValueOnce({
      id: 1,
      verb: "skills.add",
      group: "skills",
      args: [],
      state: "failed",
      started_at: "",
      finished_at: "",
      exit_code: 1,
      log_truncated: false,
      result: {
        error: {
          code: "ref_not_found",
          field: "ref",
          message: "Ref nope not found in that repository",
        },
      },
    });
    render(
      <SkillDialog agents={[]} unsupported={[]} installed={[]} editing={null} onClose={() => {}} />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "From a repository" }));
    type("Repository", "anthropics/skills");
    type("Ref", "nope");
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));
    expect(await screen.findByText("Ref nope not found in that repository")).toBeTruthy();
    expect(screen.getByLabelText("Ref").getAttribute("aria-invalid")).toBe("true");
  });
});

describe("AgentChecklist", () => {
  it("shows reach through another agent and agents without skills", () => {
    render(
      <AgentChecklist
        value={["c"]}
        agents={[agent("c", "Claude Code"), agent("o", "OpenCode")]}
        notes={{ o: "Through Claude Code" }}
        unsupported={[agent("x", "Other")]}
        onChange={() => {}}
      />,
    );
    const through = screen.getByRole("checkbox", { name: /OpenCode/ });
    expect(through.getAttribute("aria-checked")).toBe("true");
    expect(through.hasAttribute("data-disabled")).toBe(true);
    expect(screen.getByText("Through Claude Code")).toBeTruthy();
    const other = screen.getByRole("checkbox", { name: /Other/ });
    expect(other.getAttribute("aria-checked")).toBe("false");
    expect(other.hasAttribute("data-disabled")).toBe(true);
    expect(screen.getByText("Doesn't support skills")).toBeTruthy();
  });
});
