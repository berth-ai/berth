import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/request";
import type { InstructionFile } from "@/api/types";
import { readInstructionFile } from "@/api/queries";
import { writeInstructions } from "@/api/workspaces";
import { InstructionEditor } from "./workspace-instructions";

vi.mock("@/api/workspaces", () => ({ writeInstructions: vi.fn(), mergeInstructions: vi.fn() }));
vi.mock("@/api/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/queries")>()),
  readInstructionFile: vi.fn(),
  refreshWorkspaceReport: vi.fn(),
}));
vi.mock("@/lib/notify", () => ({ notify: vi.fn() }));

const file = (over: Partial<InstructionFile> = {}): InstructionFile => ({
  file: "CLAUDE.md",
  state: "file",
  content: "old\n",
  sha256: "s1",
  size: 4,
  editable: true,
  tracked: false,
  ...over,
});

describe("InstructionEditor", () => {
  beforeEach(() => {
    vi.mocked(writeInstructions).mockReset();
    vi.mocked(readInstructionFile).mockReset();
  });

  it("saves over the version it read", async () => {
    vi.mocked(writeInstructions).mockResolvedValue("s2");
    render(
      <InstructionEditor id="api" file="CLAUDE.md" agents={["Claude Code"]} initial={file()} />,
    );
    fireEvent.change(screen.getByLabelText("CLAUDE.md"), { target: { value: "new\n" } });
    fireEvent.click(screen.getByRole("button", { name: "Save CLAUDE.md" }));
    await vi.waitFor(() =>
      expect(writeInstructions).toHaveBeenCalledWith("api", "CLAUDE.md", "new\n", "s1"),
    );
  });

  it("keeps the draft on changed_on_disk", async () => {
    vi.mocked(writeInstructions).mockRejectedValueOnce(
      new ApiError(409, {
        error: {
          code: "changed_on_disk",
          message: "CLAUDE.md changed on disk since you opened it",
        },
      }),
    );
    vi.mocked(readInstructionFile).mockResolvedValue(file({ content: "theirs\n", sha256: "s9" }));
    render(<InstructionEditor id="api" file="CLAUDE.md" agents={[]} initial={file()} />);
    fireEvent.change(screen.getByLabelText("CLAUDE.md"), { target: { value: "mine\n" } });
    fireEvent.click(screen.getByRole("button", { name: "Save CLAUDE.md" }));
    expect(await screen.findByText(/changed on disk since you opened it/)).toBeTruthy();
    expect((screen.getByLabelText("CLAUDE.md") as HTMLTextAreaElement).value).toBe("mine\n");

    vi.mocked(writeInstructions).mockResolvedValue("s10");
    fireEvent.click(screen.getByRole("button", { name: "Use my version" }));
    await vi.waitFor(() =>
      expect(writeInstructions).toHaveBeenLastCalledWith("api", "CLAUDE.md", "mine\n", "s9"),
    );
  });

  it("discards the draft for the version on disk", async () => {
    vi.mocked(writeInstructions).mockRejectedValueOnce(
      new ApiError(409, { error: { code: "changed_on_disk", message: "changed" } }),
    );
    vi.mocked(readInstructionFile).mockResolvedValue(file({ content: "theirs\n", sha256: "s9" }));
    render(<InstructionEditor id="api" file="CLAUDE.md" agents={[]} initial={file()} />);
    fireEvent.change(screen.getByLabelText("CLAUDE.md"), { target: { value: "mine\n" } });
    fireEvent.click(screen.getByRole("button", { name: "Save CLAUDE.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Discard my edits" }));
    await vi.waitFor(() =>
      expect((screen.getByLabelText("CLAUDE.md") as HTMLTextAreaElement).value).toBe("theirs\n"),
    );
  });

  it("shows a file it cannot edit without an editor", () => {
    render(
      <InstructionEditor
        id="api"
        file="CLAUDE.md"
        agents={[]}
        initial={file({
          editable: false,
          reason: "CLAUDE.md is larger than 256 KiB; edit it in the IDE",
          content: "",
        })}
      />,
    );
    expect(screen.getByText(/larger than 256 KiB/)).toBeTruthy();
    expect(screen.queryByLabelText("CLAUDE.md")).toBeNull();
  });
});
