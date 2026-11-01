import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { dayTitle } from "@/lib/format";
import { UsageBars } from "./usage-bars";

const days = [
  { date: "2026-10-30", total: 0 },
  { date: "2026-10-31", total: 500 },
  { date: "2026-11-01", total: 2_000_000 },
];

describe("UsageBars", () => {
  it("scales bars to the busiest day and leaves an idle day empty", () => {
    render(<UsageBars days={days} total={2_000_500} />);
    const bars = screen.getAllByRole("listitem");
    expect(bars).toHaveLength(3);
    const height = (i: number) => (bars[i].firstElementChild as HTMLElement).style.height;
    expect(height(0)).toBe("0px");
    expect(height(1)).toBe("max(2px, 0.025%)");
    expect(height(2)).toBe("max(2px, 100%)");
    expect(bars[2].getAttribute("aria-label")).toBe(`${dayTitle("2026-11-01")}: 2,000,000 tokens`);
  });

  it("reads out the total, then the focused or hovered day", () => {
    render(<UsageBars days={days} total={2_000_500} />);
    expect(screen.getByText("Last 3 days").parentElement?.textContent).toBe(
      "Last 3 days · 2M tokens",
    );
    const bars = screen.getAllByRole("listitem");
    fireEvent.focus(bars[1]);
    expect(screen.getByText(dayTitle("2026-10-31")).parentElement?.textContent).toContain(
      "· 500 tokens",
    );
    fireEvent.blur(bars[1]);
    fireEvent.pointerEnter(bars[2]);
    expect(screen.getByText(dayTitle("2026-11-01"))).toBeTruthy();
    fireEvent.pointerLeave(screen.getByRole("list"));
    expect(screen.getByText("Last 3 days")).toBeTruthy();
  });
});
