import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentPackage, Job } from "@/api/types";
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

const done = (verb: string, result: Job["result"], state = "succeeded"): Job => ({
  id: 1,
  verb,
  group: "skills",
  args: [],
  state,
  started_at: "",
  finished_at: "",
  exit_code: state === "succeeded" ? 0 : 1,
  log_truncated: false,
  result,
});

const COMMIT = "0123456789abcdef0123456789abcdef01234567";
const scanned = done("skills.scan", {
  commit: COMMIT,
  skills: [
    { path: "skills/a", name: "a", description: "Alpha" },
    { path: "skills/b", name: "b", description: "Beta", installed: true },
    { path: "skills/c", error: "SKILL.md has no description" },
    { path: "skills/d", name: "d", description: "Delta" },
  ],
});

const dialog = (props: Partial<Parameters<typeof SkillDialog>[0]> = {}) =>
  render(<SkillDialog agents={[]} unsupported={[]} installed={[]} onClose={() => {}} {...props} />);

const button = (name: string | RegExp) => screen.getByRole<HTMLButtonElement>("button", { name });
const box = (name: RegExp) => screen.getByRole("checkbox", { name });

async function findSkills() {
  fireEvent.click(await screen.findByRole("button", { name: "From a repository" }));
  type("Repository", "acme/eng-skills");
  fireEvent.click(button("Find skills"));
  await screen.findByText(/4 skills at/);
}

describe("SkillDialog", () => {
  beforeEach(async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockReset();
  });

  it("validates the repository form before scanning", async () => {
    const { runJob } = await import("@/api/run-job");
    dialog();
    fireEvent.click(await screen.findByRole("button", { name: "From a repository" }));
    type("Repository", "http://example.com/x");
    fireEvent.click(button("Find skills"));
    expect(await screen.findByText("Start the URL with https://.")).toBeTruthy();
    expect(runJob).not.toHaveBeenCalled();
  });

  it("scans, then adds the checked skills at the scanned commit", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockImplementation(async (verb) =>
      verb === "skills.scan" ? scanned : done(verb, { changed: true, skills: ["a"] }),
    );
    const onClose = vi.fn();
    dialog({ onClose });
    await findSkills();
    expect(runJob).toHaveBeenCalledWith(
      "skills.scan",
      { repo: "https://github.com/acme/eng-skills", ref: "main", path: "" },
      expect.any(Function),
    );
    expect(screen.getByText("0123456789ab")).toBeTruthy();
    expect(box(/^a/).getAttribute("aria-checked")).toBe("true");
    expect(box(/^d/).getAttribute("aria-checked")).toBe("true");
    expect(box(/^b/).hasAttribute("data-disabled")).toBe(true);
    expect(box(/^b/).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByText("Already installed")).toBeTruthy();
    expect(box(/skills\/c/).hasAttribute("data-disabled")).toBe(true);
    expect(screen.getByText("SKILL.md has no description")).toBeTruthy();

    fireEvent.click(button("Select none"));
    expect(button("Add 0 skills").disabled).toBe(true);
    fireEvent.click(button("Select all"));
    expect(button("Add 2 skills").disabled).toBe(false);
    fireEvent.click(box(/^d/));
    fireEvent.click(button("Add 1 skill"));
    await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(runJob).toHaveBeenLastCalledWith(
      "skills.add",
      {
        repo: "https://github.com/acme/eng-skills",
        ref: "main",
        commit: COMMIT,
        paths: ["skills/a"],
        agents: null,
      },
      expect.any(Function),
    );
  });

  it("keeps Add disabled when nothing can be added", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(
      done("skills.scan", {
        commit: COMMIT,
        skills: [
          { path: "skills/b", name: "b", description: "Beta", installed: true },
          { path: "skills/c", error: "SKILL.md has no description" },
        ],
      }),
    );
    dialog();
    fireEvent.click(await screen.findByRole("button", { name: "From a repository" }));
    type("Repository", "acme/eng-skills");
    fireEvent.click(button("Find skills"));
    await screen.findByText(/2 skills at/);
    expect(button("Add 0 skills").disabled).toBe(true);
    fireEvent.click(button("Select all"));
    expect(button("Add 0 skills").disabled).toBe(true);
  });

  it("goes back to the form with its fields kept", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(scanned);
    dialog();
    await findSkills();
    fireEvent.click(button("Back"));
    expect(screen.getByLabelText<HTMLInputElement>("Repository").value).toBe("acme/eng-skills");
    expect(button("Find skills")).toBeTruthy();
  });

  it("shows a scan's field error on that field", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(
      done(
        "skills.scan",
        {
          error: {
            code: "ref_not_found",
            field: "ref",
            message: "Ref nope not found in that repository",
          },
        },
        "failed",
      ),
    );
    dialog();
    fireEvent.click(await screen.findByRole("button", { name: "From a repository" }));
    type("Repository", "anthropics/skills");
    type("Ref", "nope");
    fireEvent.click(button("Find skills"));
    expect(await screen.findByText("Ref nope not found in that repository")).toBeTruthy();
    expect(screen.getByLabelText("Ref").getAttribute("aria-invalid")).toBe("true");
  });

  it("adds a catalog skill by scanning it first", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockImplementation(async (verb) =>
      verb === "skills.scan"
        ? done(verb, {
            commit: COMMIT,
            skills: [{ path: "skills/pdf", name: "pdf", description: "PDF" }],
          })
        : done(verb, { changed: true, skills: ["pdf"] }),
    );
    const onClose = vi.fn();
    dialog({ onClose });
    fireEvent.click(await screen.findByRole("button", { name: "Add pdf" }));
    await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(vi.mocked(runJob).mock.calls.map((c) => [c[0], c[1]])).toEqual([
      [
        "skills.scan",
        { repo: "https://github.com/anthropics/skills", ref: "main", path: "skills/pdf" },
      ],
      [
        "skills.add",
        {
          repo: "https://github.com/anthropics/skills",
          ref: "main",
          commit: COMMIT,
          paths: ["skills/pdf"],
          agents: null,
        },
      ],
    ]);
  });

  it("greys out catalog skills that are installed", async () => {
    dialog({ installed: ["pdf"] });
    expect(
      (await screen.findByRole<HTMLButtonElement>("button", { name: /Add pdf/ })).disabled,
    ).toBe(true);
    expect(button(/Add docx/).disabled).toBe(false);
  });

  it("opens on a source and scans it at once", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(scanned);
    dialog({
      source: { repo: "https://github.com/acme/eng-skills", ref: "v1.2.0", path: "skills" },
    });
    await screen.findByText(/4 skills at/);
    expect(runJob).toHaveBeenCalledWith(
      "skills.scan",
      { repo: "https://github.com/acme/eng-skills", ref: "v1.2.0", path: "skills" },
      expect.any(Function),
    );
    expect(button("Add 2 skills")).toBeTruthy();
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
