import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Job, Skill } from "@/api/types";
import { RefDialog } from "./ref-dialog";

vi.mock("@/api/run-job", () => ({ runJob: vi.fn() }));

const done = (result: Job["result"], state = "succeeded"): Job => ({
  id: 1,
  verb: "skills.update",
  group: "skills",
  args: [],
  state,
  started_at: "",
  finished_at: "",
  exit_code: state === "succeeded" ? 0 : 1,
  log_truncated: false,
  result,
});

const acme: Skill = {
  repo: "https://github.com/acme/eng-skills",
  path: "skills/a",
  ref: "main",
  commit: "0123456789abcdef0123456789abcdef01234567",
  description: "Alpha",
  agents: null,
  enabled: true,
};
const group = [
  { id: "a", skill: acme },
  { id: "d", skill: { ...acme, path: "skills/d" } },
];

const type = (value: string) =>
  fireEvent.change(screen.getByLabelText("Ref"), { target: { value } });
const save = () => fireEvent.click(screen.getByRole("button", { name: "Save" }));

describe("RefDialog", () => {
  beforeEach(() => vi.clearAllMocks());

  async function expectUpdate(ids: string[], ref: string) {
    const { runJob } = await import("@/api/run-job");
    await vi.waitFor(() =>
      expect(runJob).toHaveBeenCalledWith("skills.update", { ids, ref }, expect.any(Function)),
    );
  }

  it("moves the whole group when asked to by default", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(done({ changed: true, skills: ["a", "d"] }));
    render(<RefDialog row={group[0]} group={group} defaultAll onClose={() => {}} />);
    expect(screen.getByRole("heading", { name: "Change ref for a" })).toBeTruthy();
    const all = screen.getByRole("checkbox", {
      name: "Apply to all 2 skills from acme/eng-skills",
    });
    expect(all.getAttribute("aria-checked")).toBe("true");
    type("v2");
    save();
    await expectUpdate(["a", "d"], "v2");
  });

  it("moves only this skill when unticked", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(done({ changed: true, skills: ["a"] }));
    render(<RefDialog row={group[0]} group={group} defaultAll onClose={() => {}} />);
    fireEvent.click(screen.getByRole("checkbox", { name: /Apply to all/ }));
    type("v2");
    save();
    await expectUpdate(["a"], "v2");
  });

  it("offers no group option for a single skill", () => {
    render(<RefDialog row={group[0]} group={[group[0]]} onClose={() => {}} />);
    expect(screen.queryByRole("checkbox", { name: /Apply to all/ })).toBeNull();
  });

  it("changes a whole source without asking", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(done({ changed: true, skills: ["a", "d"] }));
    render(<RefDialog row={group[0]} group={group} whole onClose={() => {}} />);
    expect(screen.getByRole("heading", { name: "Change ref for acme/eng-skills" })).toBeTruthy();
    expect(screen.getByText(/Applies to all 2 skills from this source\./)).toBeTruthy();
    expect(screen.queryByRole("checkbox")).toBeNull();
    type("v2");
    save();
    await expectUpdate(["a", "d"], "v2");
  });

  it("checks the ref before starting a job", async () => {
    const { runJob } = await import("@/api/run-job");
    render(<RefDialog row={group[0]} group={[group[0]]} onClose={() => {}} />);
    type("bad ref");
    save();
    expect(await screen.findByText("Enter a branch, tag or commit.")).toBeTruthy();
    expect(runJob).not.toHaveBeenCalled();
  });

  it("puts a failed fetch under the ref", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(
      done(
        {
          error: { code: "invalid", field: "ref", message: "Ref v9 not found in that repository." },
        },
        "failed",
      ),
    );
    render(<RefDialog row={group[0]} group={[group[0]]} onClose={() => {}} />);
    type("v9");
    save();
    expect(await screen.findByText("Ref v9 not found in that repository.")).toBeTruthy();
  });
});
