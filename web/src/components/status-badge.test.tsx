import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusBadge, StatusIcon } from "./status-badge";

describe("StatusBadge", () => {
  it.each([
    ["running", "Running"],
    ["failed", "Failed"],
    ["outdated", "Update available"],
    ["in_progress", "Running"],
    ["signed_out", "Signed out"],
    ["not_installed", "Not installed"],
    ["checking", "Checking"],
    ["something-new", "Unknown"],
  ])("shows %s as a word", (state, word) => {
    render(<StatusBadge state={state} />);
    expect(screen.getByText(word)).toBeTruthy();
  });

  it("adds the detail in muted text", () => {
    render(<StatusBadge state="restarting" detail="3 restarts" />);
    expect(screen.getByText("3 restarts")).toBeTruthy();
  });
});

describe("StatusIcon", () => {
  it("names the status in words and shows details on demand", async () => {
    render(
      <StatusIcon state="signed_in" label="Signed in as me@example.com">
        <p>more</p>
      </StatusIcon>,
    );
    const trigger = screen.getByRole("button", { name: "Signed in as me@example.com" });
    expect(screen.queryByText("more")).toBeNull();
    fireEvent.click(trigger);
    expect(await screen.findByText("more")).toBeTruthy();
  });
});
