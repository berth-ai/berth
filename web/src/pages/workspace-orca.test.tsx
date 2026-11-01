import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { refreshOrcaStatus, useOrcaStatus } from "@/api/queries";
import { ApiError, verb } from "@/api/request";
import type { OrcaStatus } from "@/api/types";
import { OrcaFact } from "./workspace-orca";

vi.mock(import("@/api/queries"), async (importOriginal) => ({
  ...(await importOriginal()),
  useOrcaStatus: vi.fn() as never,
  refreshOrcaStatus: vi.fn() as never,
}));
vi.mock(import("@/api/request"), async (importOriginal) => ({
  ...(await importOriginal()),
  verb: vi.fn() as never,
}));
vi.mock("@/lib/notify", () => ({ notify: vi.fn() }));

const answer = (q: Partial<{ data: OrcaStatus; isError: boolean; error: unknown }>) =>
  vi.mocked(useOrcaStatus).mockReturnValue(q as never);

const report = (api: OrcaStatus["workspaces"][string], syncing = false): OrcaStatus => ({
  workspaces: { api },
  synced_at: null,
  syncing,
});

describe("OrcaFact", () => {
  beforeEach(() => {
    vi.mocked(verb).mockReset();
    vi.mocked(refreshOrcaStatus).mockReset();
  });

  it("says a workspace is in Orca, with no Sync now", () => {
    answer({ data: report({ path: "/workspaces/acme/api", in_orca: true }) });
    render(<OrcaFact id="api" />);
    expect(screen.getByText("In Orca")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Sync now" })).toBeNull();
  });

  it("offers Sync now for a workspace Orca lacks, and syncs", async () => {
    answer({ data: report({ path: "/workspaces/acme/api", in_orca: false }) });
    vi.mocked(verb).mockResolvedValue({ syncing: true });
    render(<OrcaFact id="api" />);
    expect(screen.getByText("Not in Orca")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Sync now" }));
    await vi.waitFor(() => expect(refreshOrcaStatus).toHaveBeenCalled());
    expect(verb).toHaveBeenCalledWith("orca.sync");
  });

  it("shows why the last sync could not add it", () => {
    answer({
      data: report({ path: "/workspaces/acme/api", in_orca: false, error: "not a git repository" }),
    });
    render(<OrcaFact id="api" />);
    expect(screen.getByText("Not added")).toBeTruthy();
    expect(screen.getByText(/could not add it: not a git repository/)).toBeTruthy();
  });

  it("says when Orca isn't answering", () => {
    answer({
      isError: true,
      error: new ApiError(502, {
        error: { code: "orca_unavailable", message: "no answer from Orca after 0s" },
      }),
    });
    render(<OrcaFact id="api" />);
    expect(screen.getByText("Orca isn't answering yet.")).toBeTruthy();
  });
});
