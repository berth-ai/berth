import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Job } from "@/api/types";
import { DeleteFolderDialog } from "./workspace";

vi.mock("@/api/run-job", () => ({ runJob: vi.fn() }));
vi.mock("@/api/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/queries")>()),
  refreshWorkspaces: vi.fn(),
}));

const job = (state: string, result: Job["result"]): Job => ({
  id: 2,
  verb: "workspaces.forget",
  group: "workspaces",
  args: [],
  state,
  started_at: "",
  finished_at: "",
  exit_code: state === "succeeded" ? 0 : 1,
  log_truncated: false,
  result,
  log: "",
});

const confirmField = () => screen.getByLabelText("Type acme/api to confirm");

describe("DeleteFolderDialog", () => {
  beforeEach(async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockReset();
  });

  it("deletes only once the folder's path is typed", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(job("succeeded", { deleted: true }));
    const onDone = vi.fn();
    render(<DeleteFolderDialog id="api" path="acme/api" onClose={() => {}} onDone={onDone} />);
    const del = screen.getByRole<HTMLButtonElement>("button", { name: "Delete folder" });
    expect(del.disabled).toBe(true);
    fireEvent.change(confirmField(), { target: { value: "acme" } });
    expect(del.disabled).toBe(true);
    fireEvent.change(confirmField(), { target: { value: "acme/api" } });
    expect(del.disabled).toBe(false);
    fireEvent.click(del);
    await vi.waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(runJob).toHaveBeenCalledWith(
      "workspaces.forget",
      { id: "api", delete: true, confirm: "acme/api" },
      expect.any(Function),
    );
  });

  it("lists what it could not delete and then only offers Done", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockImplementation(async (_verb, _body, onUpdate) => {
      const j = job("failed", {
        error: {
          code: "delete_incomplete",
          message:
            "Berth forgot api, but agent could not delete everything in /workspaces/acme/api",
          left: ["acme/api/locked/f"],
        },
      });
      onUpdate?.(j);
      return j;
    });
    const onDone = vi.fn();
    render(<DeleteFolderDialog id="api" path="acme/api" onClose={() => {}} onDone={onDone} />);
    fireEvent.change(confirmField(), { target: { value: "acme/api" } });
    fireEvent.click(screen.getByRole("button", { name: "Delete folder" }));
    expect(await screen.findByText("acme/api/locked/f")).toBeTruthy();
    expect(screen.getByText(/could not delete everything/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Delete folder" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(onDone).toHaveBeenCalled();
  });
});
