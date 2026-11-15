import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { applyConfig } from "@/api/config";
import { ApiError } from "@/api/request";
import { FrontendOff } from "./frontend";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));
vi.mock("@/api/config", () => ({ applyConfig: vi.fn() }));
vi.mock("@/api/queries", () => ({
  keys: { config: ["config"] },
  queryClient: { invalidateQueries: vi.fn() },
  refreshStatus: vi.fn(),
}));
vi.mock("@/lib/notify", () => ({ notify: vi.fn() }));

describe("FrontendOff", () => {
  beforeEach(() => vi.mocked(applyConfig).mockReset());

  it("turns the frontend on in place", async () => {
    vi.mocked(applyConfig).mockResolvedValue({} as never);
    render(<FrontendOff label="IDE" frontend="ide" />);
    fireEvent.click(screen.getByRole("button", { name: "Turn on the IDE" }));
    await vi.waitFor(() =>
      expect(applyConfig).toHaveBeenCalledWith({ frontends: { ide: { enabled: true } } }),
    );
    expect(screen.getByRole("link", { name: "Open Settings" }).getAttribute("href")).toBe(
      "/settings",
    );
  });

  it("shows why it could not turn on", async () => {
    vi.mocked(applyConfig).mockRejectedValueOnce(
      new ApiError(400, { error: { code: "invalid", message: "Set a pairing address first." } }),
    );
    render(<FrontendOff label="Orca server" frontend="orca" />);
    fireEvent.click(screen.getByRole("button", { name: "Turn on the Orca server" }));
    expect(await screen.findByText(/Set a pairing address first/)).toBeTruthy();
  });
});
