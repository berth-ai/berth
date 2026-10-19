import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { applyConfig } from "@/api/config";
import { applyAndNotify } from "./apply-notify";
import { EnvDialog } from "./workspace-env";

vi.mock("./apply-notify", () => ({ applyAndNotify: vi.fn(async () => true), BG: "" }));
vi.mock("@/api/config", () => ({ applyConfig: vi.fn(async () => ({})) }));
vi.mock("@/lib/notify", () => ({ notify: vi.fn() }));

describe("EnvDialog", () => {
  beforeEach(() => {
    vi.mocked(applyAndNotify).mockClear();
    vi.mocked(applyConfig).mockClear();
  });

  it("adds a plain variable", async () => {
    const onClose = vi.fn();
    render(<EnvDialog id="api" existing={["LOG"]} onClose={onClose} />);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "LOG" } });
    expect(screen.getByText(/LOG exists/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "DEBUG" } });
    fireEvent.change(screen.getByLabelText("Value"), { target: { value: "1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add variable" }));
    await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(applyAndNotify).toHaveBeenCalledWith(
      { workspaces: { api: { env: { DEBUG: { value: "1", secret: false } } } } },
      "DEBUG is set in api; new windows get it.",
    );
  });

  it("adds a secret in one change, value outside the patch", async () => {
    render(<EnvDialog id="api" existing={[]} onClose={vi.fn()} />);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "STRIPE_KEY" } });
    fireEvent.click(screen.getByRole("checkbox", { name: "Secret" }));
    fireEvent.change(screen.getByLabelText("Value"), { target: { value: "sk_live" } });
    fireEvent.click(screen.getByRole("button", { name: "Add variable" }));
    await vi.waitFor(() =>
      expect(applyConfig).toHaveBeenCalledWith(
        { workspaces: { api: { env: { STRIPE_KEY: { value: "", secret: true } } } } },
        { "workspaces.api.STRIPE_KEY": "sk_live" },
      ),
    );
  });

  it("replaces a secret's value without a patch", async () => {
    render(
      <EnvDialog
        id="api"
        existing={["KEY"]}
        edit={{ key: "KEY", secret: true, value: "", set: true }}
        onClose={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByLabelText("Value"), { target: { value: "new" } });
    fireEvent.click(screen.getByRole("button", { name: "Replace" }));
    await vi.waitFor(() =>
      expect(applyConfig).toHaveBeenCalledWith(null, { "workspaces.api.KEY": "new" }),
    );
  });
});
