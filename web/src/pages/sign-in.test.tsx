import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentPackage } from "@/api/types";
import { SignInCell } from "./sign-in";

const pkg: AgentPackage = {
  package: "@a/cli",
  name: "Agent A",
  kind: "agent",
  version: "1",
  enabled: true,
  bins: [],
  keys: [],
};

describe("SignInCell", () => {
  it("offers sign in, sign in again and sign out per method", () => {
    const onSignIn = vi.fn();
    const onSignOut = vi.fn();
    render(
      <SignInCell
        pkg={pkg}
        status={{
          name: "Agent A",
          methods: [
            {
              id: "c",
              label: "ChatGPT",
              flow: "device",
              signed_in: true,
              account: "you@example.org",
              error: "",
            },
            {
              id: "g",
              label: "GitHub Copilot",
              flow: "device",
              signed_in: false,
              account: "",
              error: "",
            },
          ],
        }}
        loading={false}
        failed={false}
        onSignIn={onSignIn}
        onSignOut={onSignOut}
      />,
    );
    expect(screen.getByText("you@example.org")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Sign Agent A out of ChatGPT" }));
    expect(onSignOut).toHaveBeenCalledWith(expect.objectContaining({ id: "c" }));
    fireEvent.click(screen.getByRole("button", { name: "Sign in Agent A with ChatGPT again" }));
    fireEvent.click(screen.getByRole("button", { name: "Sign in Agent A with GitHub Copilot" }));
    expect(onSignIn.mock.calls.map((c) => c[0].id)).toEqual(["c", "g"]);
  });

  it("shows a dash, a probe error, or Unavailable", () => {
    const { rerender } = render(
      <SignInCell
        pkg={{ ...pkg, enabled: false }}
        status={undefined}
        loading={false}
        failed={false}
        onSignIn={vi.fn()}
        onSignOut={vi.fn()}
      />,
    );
    expect(screen.getByText("—")).toBeTruthy();
    rerender(
      <SignInCell
        pkg={pkg}
        status={{
          name: "Agent A",
          methods: [
            {
              id: "c",
              label: "C",
              flow: "device",
              signed_in: false,
              account: "",
              error: "timed out",
            },
          ],
        }}
        loading={false}
        failed={false}
        onSignIn={vi.fn()}
        onSignOut={vi.fn()}
      />,
    );
    expect(screen.getByText("timed out")).toBeTruthy();
    expect(screen.getByText("Unknown").closest("[class*=warn]")).toBeTruthy();
    rerender(
      <SignInCell
        pkg={pkg}
        status={undefined}
        loading={false}
        failed
        onSignIn={vi.fn()}
        onSignOut={vi.fn()}
      />,
    );
    expect(screen.getByText("Unavailable")).toBeTruthy();
  });
});
