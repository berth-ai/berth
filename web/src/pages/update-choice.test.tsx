import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { UpdateChoice } from "./update-choice";

describe("UpdateChoice", () => {
  it("updates the whole group or only this skill", () => {
    const onPick = vi.fn();
    render(
      <UpdateChoice id="pdf" ids={["docx", "pdf", "xlsx"]} onPick={onPick} onClose={() => {}} />,
    );
    expect(screen.getByRole("alertdialog", { name: "Update pdf?" })).toBeTruthy();
    expect(screen.getByText(/2 other skills/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Update all 3" }));
    expect(onPick).toHaveBeenLastCalledWith(["docx", "pdf", "xlsx"]);
    fireEvent.click(screen.getByRole("button", { name: "Only pdf" }));
    expect(onPick).toHaveBeenLastCalledWith(["pdf"]);
  });
});
