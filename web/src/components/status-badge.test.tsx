import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusBadge } from "./status-badge";

describe("StatusBadge", () => {
  it.each([
    ["running", "Running"],
    ["failed", "Failed"],
    ["outdated", "Update available"],
    ["in_progress", "Running"],
    ["signed_out", "Signed out"],
    ["not_installed", "Not installed"],
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
