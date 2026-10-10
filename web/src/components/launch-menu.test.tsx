import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LaunchMenu } from "./launch-menu";

describe("LaunchMenu", () => {
  it("is a labelled button that can be disabled", () => {
    render(<LaunchMenu label="Launch in api" agents={[]} disabled onLaunch={() => {}} />);
    const button = screen.getByRole<HTMLButtonElement>("button", { name: "Launch in api" });
    expect(button.textContent).toContain("Launch");
    expect(button.disabled).toBe(true);
  });
});
