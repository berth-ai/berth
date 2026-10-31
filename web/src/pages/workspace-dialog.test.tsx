import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Config, Job } from "@/api/types";
import { AddWorkspaceDialog } from "./workspace-dialog";

vi.mock("@/api/run-job", () => ({ runJob: vi.fn() }));
vi.mock("@/api/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/queries")>()),
  refreshWorkspaces: vi.fn(),
  useCandidates: vi.fn(() => ({
    data: {
      candidates: [
        { path: "scratch", git: false },
        { path: "vendor/tool", git: true },
      ],
    },
    isPending: false,
    error: null,
  })),
}));

const cfg = { workspaces: { api: { path: "acme/api", repo: "" } } } as unknown as Config;

const job = (state: string, result: Job["result"]): Job => ({
  id: 1,
  verb: "workspaces.add",
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

const dialog = (props: Partial<Parameters<typeof AddWorkspaceDialog>[0]> = {}) =>
  render(<AddWorkspaceDialog config={cfg} onClose={() => {}} onAdded={() => {}} {...props} />);

const field = (label: string) => screen.getByLabelText<HTMLInputElement>(label);
const type = (label: string, value: string) =>
  fireEvent.change(field(label), { target: { value } });

describe("AddWorkspaceDialog", () => {
  beforeEach(async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockReset();
  });

  it("derives the folder from the repository and the id from the folder", () => {
    dialog();
    type("Repository", "octocat/Hello-World");
    expect(field("Folder").value).toBe("github.com/octocat/Hello-World");
    expect(field("Id").value).toBe("octocat-hello-world");
    type("Folder", "demo/hello");
    expect(field("Id").value).toBe("demo-hello");
    type("Id", "hello");
    type("Folder", "demo/other");
    expect(field("Id").value).toBe("hello");
  });

  it("clones with the normalized repository and reports the id", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(job("succeeded", { id: "acme-web", changed: true }));
    const onAdded = vi.fn();
    dialog({ onAdded });
    type("Repository", "acme/web");
    type("Ref", " v2 ");
    fireEvent.click(screen.getByRole("button", { name: "Clone" }));
    await vi.waitFor(() => expect(onAdded).toHaveBeenCalledWith("acme-web"));
    expect(runJob).toHaveBeenCalledWith(
      "workspaces.add",
      {
        mode: "clone",
        repo: "https://github.com/acme/web",
        ref: "v2",
        path: "github.com/acme/web",
        id: "acme-web",
      },
      expect.any(Function),
    );
  });

  it("checks every field before starting a job", async () => {
    const { runJob } = await import("@/api/run-job");
    dialog();
    type("Repository", "http://example.com/a/b");
    type("Folder", "acme");
    type("Id", "api");
    fireEvent.click(screen.getByRole("button", { name: "Clone" }));
    expect(await screen.findByText("Start the URL with https://.")).toBeTruthy();
    expect(
      screen.getByText("This folder overlaps workspace api (acme/api); workspaces cannot nest."),
    ).toBeTruthy();
    expect(screen.getByText("A workspace named api exists; choose another id.")).toBeTruthy();
    expect(runJob).not.toHaveBeenCalled();
  });

  it("puts a failed clone's error on its field and stays open", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(
      job("failed", {
        error: {
          code: "ref_not_found",
          field: "ref",
          message: "Ref v9 not found in that repository",
        },
      }),
    );
    const onAdded = vi.fn();
    dialog({ onAdded });
    type("Repository", "acme/web");
    type("Ref", "v9");
    fireEvent.click(screen.getByRole("button", { name: "Clone" }));
    expect(await screen.findByText("Ref v9 not found in that repository")).toBeTruthy();
    expect(field("Ref").getAttribute("aria-invalid")).toBe("true");
    expect(onAdded).not.toHaveBeenCalled();
  });

  it("adopts a listed folder", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(job("succeeded", { id: "vendor-tool", writable: true }));
    const onAdded = vi.fn();
    dialog({ onAdded });
    fireEvent.click(screen.getByRole("button", { name: "Adopt a folder" }));
    fireEvent.click(screen.getByRole("button", { name: "Add workspace" }));
    expect(await screen.findByText("Choose a folder.")).toBeTruthy();
    fireEvent.click(screen.getByRole("radio", { name: /vendor\/tool/ }));
    expect(field("Id").value).toBe("vendor-tool");
    fireEvent.click(screen.getByRole("button", { name: "Add workspace" }));
    await vi.waitFor(() => expect(onAdded).toHaveBeenCalledWith("vendor-tool"));
    expect(runJob).toHaveBeenCalledWith(
      "workspaces.add",
      { mode: "adopt", path: "vendor/tool", id: "vendor-tool" },
      expect.any(Function),
    );
  });
});
