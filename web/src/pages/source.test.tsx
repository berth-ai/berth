import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Job, Source, SourceDiff } from "@/api/types";
import { RemoveSourceDialog, UpdateDialog } from "./source";

vi.mock("@/api/run-job", () => ({ runJob: vi.fn() }));
vi.mock("@/api/request", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/request")>()),
  verb: vi.fn(),
}));

const C1 = "0123456789abcdef0123456789abcdef01234567";
const C2 = "89abcdef0123456789abcdef0123456789abcdef";
const source: Source = {
  repo: "https://github.com/acme/eng-skills",
  ref: "main",
  path: "skills",
  commit: C1,
  found: { changelog: { path: "skills/changelog", description: "Changelog" } },
  attached: { changelog: { enabled: true, agents: null, workspaces: {} } },
};
const done = (verb: string, result: unknown, state = "succeeded"): Job => ({
  id: 1,
  verb,
  group: "skills",
  args: [],
  state,
  started_at: "",
  finished_at: "",
  exit_code: 0,
  log_truncated: false,
  result: result as Job["result"],
});
const diff = (over: Partial<SourceDiff> = {}): SourceDiff => ({
  id: "eng",
  ref: "main",
  from: C1,
  commit: C2,
  changed: true,
  added: [{ name: "triage", path: "skills/triage", description: "Triage", attached: false }],
  removed: [
    { name: "changelog", path: "skills/changelog", description: "Changelog", attached: true },
  ],
  moved: [],
  described: [],
  ...over,
});
const button = (name: string) => screen.getByRole<HTMLButtonElement>("button", { name });

describe("UpdateDialog", () => {
  beforeEach(async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockReset();
  });

  it("previews at once and says when nothing moved", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(
      done("skills.sources.preview", diff({ commit: C1, changed: false, added: [], removed: [] })),
    );
    render(<UpdateDialog id="eng" source={source} changeRef={false} onClose={() => {}} />);
    expect(await screen.findByText(/Already up to date/)).toBeTruthy();
    expect(runJob).toHaveBeenCalledWith(
      "skills.sources.preview",
      { id: "eng", ref: "main" },
      expect.any(Function),
    );
    // The footer says Close, next to the dialog's own close button.
    expect(screen.getAllByRole("button", { name: "Close" })).toHaveLength(2);
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
  });

  it("shows the diff, warns about attached skills that go missing, and applies the previewed commit", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockImplementation(async (verb) =>
      verb === "skills.sources.preview"
        ? done(verb, diff())
        : done(verb, { id: "eng", commit: C2, changed: true, missing: ["changelog"] }),
    );
    const onClose = vi.fn();
    render(<UpdateDialog id="eng" source={source} changeRef={false} onClose={onClose} />);
    expect(await screen.findByRole("region", { name: "Removed" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "Added" }).textContent).toContain("triage");
    expect(screen.getByText(/changelog is attached and will be missing/)).toBeTruthy();
    fireEvent.click(button("Update to 89abcdef0123"));
    await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(runJob).toHaveBeenLastCalledWith(
      "skills.sources.update",
      { id: "eng", ref: "main", commit: C2 },
      expect.any(Function),
    );
  });

  it("asks for the new ref first when changing it", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(done("skills.sources.preview", diff({ ref: "v2" })));
    render(<UpdateDialog id="eng" source={source} changeRef onClose={() => {}} />);
    expect(runJob).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Ref"), { target: { value: "a..b" } });
    fireEvent.click(button("Preview"));
    expect(await screen.findByText("Enter a branch, tag or commit.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Ref"), { target: { value: "v2" } });
    fireEvent.click(button("Preview"));
    expect(await screen.findByRole("region", { name: "Added" })).toBeTruthy();
    expect(runJob).toHaveBeenCalledWith(
      "skills.sources.preview",
      { id: "eng", ref: "v2" },
      expect.any(Function),
    );
  });

  it("shows a failed preview", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(
      done(
        "skills.sources.preview",
        { error: { code: "auth_required", message: "Repository not found, or it is private." } },
        "failed",
      ),
    );
    render(<UpdateDialog id="eng" source={source} changeRef={false} onClose={() => {}} />);
    expect(await screen.findByText("Repository not found, or it is private.")).toBeTruthy();
  });
});

describe("RemoveSourceDialog", () => {
  it("asks for the id when skills are attached", async () => {
    const { verb } = await import("@/api/request");
    vi.mocked(verb).mockResolvedValue({ id: "eng", revision: 4 });
    const onRemoved = vi.fn();
    render(
      <RemoveSourceDialog id="eng" source={source} onClose={() => {}} onRemoved={onRemoved} />,
    );
    expect(button("Remove source").disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Type eng to confirm"), { target: { value: "eng" } });
    fireEvent.click(button("Remove source"));
    await vi.waitFor(() => expect(onRemoved).toHaveBeenCalled());
    expect(verb).toHaveBeenCalledWith("skills.sources.remove", { id: "eng" });
  });

  it("removes a source with nothing attached at once", () => {
    render(
      <RemoveSourceDialog
        id="eng"
        source={{ ...source, attached: {} }}
        onClose={() => {}}
        onRemoved={() => {}}
      />,
    );
    expect(screen.queryByLabelText("Type eng to confirm")).toBeNull();
    expect(button("Remove source").disabled).toBe(false);
  });
});
