import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Job } from "@/api/types";
import { SourceDialog } from "./source-dialog";

vi.mock("@/api/run-job", () => ({ runJob: vi.fn() }));

const COMMIT = "0123456789abcdef0123456789abcdef01234567";
const done = (verb: string, result: unknown, state = "succeeded"): Job => ({
  id: 1,
  verb,
  group: "skills",
  args: [],
  state,
  started_at: "",
  finished_at: "",
  exit_code: state === "succeeded" ? 0 : 1,
  log_truncated: false,
  result: result as Job["result"],
});
const scanned = (exists = "") =>
  done("skills.sources.preview", {
    commit: COMMIT,
    id: "acme-eng-skills",
    exists,
    found: {
      changelog: { path: "skills/changelog", description: "Changelog" },
      broken: { path: "skills/broken", error: "SKILL.md has no description" },
    },
  });

const type = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
const button = (name: string) => screen.getByRole<HTMLButtonElement>("button", { name });

describe("SourceDialog", () => {
  beforeEach(async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockReset();
  });

  it("checks the form before finding", async () => {
    const { runJob } = await import("@/api/run-job");
    render(<SourceDialog onClose={() => {}} onAdded={() => {}} />);
    type("Repository", "http://example.com/x");
    fireEvent.click(button("Find skills"));
    expect(await screen.findByText("Start the URL with https://.")).toBeTruthy();
    expect(runJob).not.toHaveBeenCalled();
  });

  it("finds, then adds the previewed commit under the suggested id", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockImplementation(async (verb) =>
      verb === "skills.sources.preview"
        ? scanned()
        : done(verb, { id: "eng", attached: [], skipped: [] }),
    );
    const onAdded = vi.fn();
    render(<SourceDialog onClose={() => {}} onAdded={onAdded} />);
    type("Repository", "acme/eng-skills");
    type("Path", "skills");
    fireEvent.click(button("Find skills"));
    expect(await screen.findByText(/1 skill at/)).toBeTruthy();
    expect(runJob).toHaveBeenCalledWith(
      "skills.sources.preview",
      { repo: "https://github.com/acme/eng-skills", ref: "main", path: "skills" },
      expect.any(Function),
    );
    expect(screen.getByText("SKILL.md has no description")).toBeTruthy();
    expect(screen.getByLabelText<HTMLInputElement>("Id").value).toBe("acme-eng-skills");
    type("Id", "eng");
    fireEvent.click(screen.getByRole("checkbox", { name: "Attach all to all agents" }));
    fireEvent.click(button("Add source"));
    await vi.waitFor(() => expect(onAdded).toHaveBeenCalledWith("eng"));
    expect(runJob).toHaveBeenLastCalledWith(
      "skills.sources.add",
      {
        repo: "https://github.com/acme/eng-skills",
        ref: "main",
        path: "skills",
        id: "eng",
        commit: COMMIT,
        attach_all: true,
      },
      expect.any(Function),
    );
  });

  it("will not add a source that already exists", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(scanned("acme-eng-skills"));
    render(<SourceDialog onClose={() => {}} onAdded={() => {}} />);
    type("Repository", "acme/eng-skills");
    fireEvent.click(button("Find skills"));
    expect(await screen.findByText(/Source acme-eng-skills already tracks/)).toBeTruthy();
    expect(button("Add source").disabled).toBe(true);
  });

  it("shows a failed find on its field", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(
      done(
        "skills.sources.preview",
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
    render(<SourceDialog onClose={() => {}} onAdded={() => {}} />);
    type("Repository", "acme/eng-skills");
    type("Ref", "nope");
    fireEvent.click(button("Find skills"));
    expect(await screen.findByText("Ref nope not found in that repository")).toBeTruthy();
  });

  it("finds at once when opened from the Suggested row", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(scanned());
    render(
      <SourceDialog
        start={{
          repo: "https://github.com/anthropics/skills",
          ref: "main",
          path: "skills",
          find: true,
        }}
        onClose={() => {}}
        onAdded={() => {}}
      />,
    );
    expect(await screen.findByText(/1 skill at/)).toBeTruthy();
    expect(runJob).toHaveBeenCalledTimes(1);
  });
});
