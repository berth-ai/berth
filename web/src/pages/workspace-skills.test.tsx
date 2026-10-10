import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Source } from "@/api/types";
import { applyAndNotify } from "./apply-notify";
import { AttachSkillDialog } from "./workspace-skills";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));
vi.mock("./apply-notify", () => ({ applyAndNotify: vi.fn(async () => true), BG: "" }));
vi.mock("@/api/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/queries")>()),
  refreshWorkspaceReport: vi.fn(),
}));

const src = (attached: Source["attached"]): Source => ({
  repo: "https://github.com/acme/skills",
  ref: "main",
  path: "",
  commit: "0123456789abcdef0123456789abcdef01234567",
  found: {
    pdf: { path: "pdf", description: "PDF tools" },
    docx: { path: "docx", description: "Word files" },
  },
  attached,
});
const SOURCES = {
  x: src({ pdf: { enabled: true, agents: [], workspaces: { api: "link" } } }),
  y: src({}),
};

describe("AttachSkillDialog", () => {
  beforeEach(() => vi.mocked(applyAndNotify).mockClear());

  it("lists every source's skills and attaches one, vendored", async () => {
    const onClose = vi.fn();
    render(<AttachSkillDialog id="api" sources={SOURCES} onClose={onClose} />);
    expect(screen.getByText("Attached")).toBeTruthy();
    expect(screen.getByText("From x")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Search skills"), { target: { value: "word" } });
    expect(screen.queryByText("From x")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Vendor" }));
    fireEvent.click(screen.getByRole("button", { name: "Attach docx from y" }));
    await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(applyAndNotify).toHaveBeenCalledWith(
      {
        skills: {
          sources: {
            y: { attached: { docx: { enabled: true, agents: [], workspaces: { api: "vendor" } } } },
          },
        },
      },
      "docx is vendored into api.",
    );
  });

  it("points to the Skills page without sources", () => {
    render(<AttachSkillDialog id="api" sources={{}} onClose={vi.fn()} />);
    expect(screen.getByText(/No skill sources yet/)).toBeTruthy();
  });
});
