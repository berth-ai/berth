import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LogViewer } from "./log-viewer";

describe("LogViewer", () => {
  it("marks error lines and the truncation note", () => {
    render(<LogViewer text={"step one\nerror: disk full\nfatal thing\n"} truncated />);
    expect(screen.getByText("error: disk full").className).toContain("text-log-err");
    expect(screen.getByText("fatal thing").className).toContain("text-log-err");
    expect(screen.getByText(/earlier output truncated/)).toBeTruthy();
    expect(screen.getByRole("log").textContent).toContain("step one\n");
  });
});
