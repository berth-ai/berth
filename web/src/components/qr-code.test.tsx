import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { QrCode } from "./qr-code";

describe("QrCode", () => {
  it("draws one path covering the dark modules", () => {
    const { container } = render(<QrCode rows={["101", "010", "101"]} label="Pairing code" />);
    const svg = screen.getByRole("img", { name: "Pairing code" });
    expect(svg.getAttribute("viewBox")).toBe("0 0 7 7");
    const paths = svg.querySelectorAll("path");
    expect(paths).toHaveLength(1);
    // Five dark modules, offset by the two-module quiet zone.
    expect(paths[0].getAttribute("d")).toBe(
      "M2 2h1v1h-1zM4 2h1v1h-1zM3 3h1v1h-1zM2 4h1v1h-1zM4 4h1v1h-1z",
    );
    expect(container.innerHTML).not.toContain("style=");
  });
});
