import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, verb } from "@/api/request";
import { OrcaBody } from "./orca";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));
vi.mock(import("@/api/request"), async (importOriginal) => ({
  ...(await importOriginal()),
  verb: vi.fn() as never,
}));
vi.mock("@/lib/notify", () => ({ notify: vi.fn() }));

const pair = (scope: string, warning?: string) => ({
  url: "orca://pair?code=dev",
  scope,
  address: "100.64.0.10:6768",
  qr: ["111", "101", "111"],
  ...(warning ? { warning } : {}),
});

describe("OrcaBody", () => {
  beforeEach(() => vi.mocked(verb).mockReset());

  it("pairs a phone and shows the link, the QR code and the replacement note", async () => {
    vi.mocked(verb).mockResolvedValue(pair("mobile"));
    render(<OrcaBody enabled state="running" address="100.64.0.10:6768" />);
    fireEvent.click(screen.getByRole("button", { name: "Pair a phone" }));
    expect(await screen.findByText("orca://pair?code=dev")).toBeTruthy();
    expect(verb).toHaveBeenCalledWith("orca.pair", { scope: "mobile" });
    expect(screen.getByRole("img", { name: "Pairing QR code for a phone" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy link" })).toBeTruthy();
    expect(screen.getByText(/A new link replaces any unused one/)).toBeTruthy();
  });

  it("pairs a desktop", async () => {
    vi.mocked(verb).mockResolvedValue(pair("desktop"));
    render(<OrcaBody enabled state="running" address="100.64.0.10:6768" />);
    fireEvent.click(screen.getByRole("button", { name: "Pair a desktop" }));
    expect(await screen.findByRole("img", { name: "Pairing QR code for a desktop" })).toBeTruthy();
    expect(verb).toHaveBeenCalledWith("orca.pair", { scope: "desktop" });
  });

  it("shows the loopback warning", async () => {
    vi.mocked(verb).mockResolvedValue(pair("desktop", "only this host can pair"));
    render(<OrcaBody enabled state="running" address="127.0.0.1:6768" />);
    fireEvent.click(screen.getByRole("button", { name: "Pair a desktop" }));
    expect(await screen.findByText("only this host can pair")).toBeTruthy();
  });

  it("shows a pairing error", async () => {
    vi.mocked(verb).mockRejectedValueOnce(
      new ApiError(504, {
        error: { code: "orca_pair_timeout", message: "Orca did not print a pairing link" },
      }),
    );
    render(<OrcaBody enabled state="running" address="100.64.0.10:6768" />);
    fireEvent.click(screen.getByRole("button", { name: "Pair a desktop" }));
    expect(await screen.findByText(/Orca did not print a pairing link/)).toBeTruthy();
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("sends the owner to Settings when Orca is off", () => {
    render(<OrcaBody enabled={false} state="disabled" address="" />);
    expect(screen.queryByRole("button", { name: /Pair a/ })).toBeNull();
    expect(screen.getByRole("link", { name: "Open Settings" }).getAttribute("href")).toBe(
      "/settings",
    );
  });
});
