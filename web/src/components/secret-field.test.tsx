import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SecretField } from "./secret-field";

describe("SecretField", () => {
  it("never shows a saved value and asks for one before saving", async () => {
    const onSave = vi
      .fn<(v: string) => Promise<string | null>>()
      .mockResolvedValueOnce("secrets.KEY: too short")
      .mockResolvedValueOnce(null);
    render(<SecretField name="KEY" isSet={false} onSave={onSave} onRemove={async () => null} />);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Paste a key first.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("KEY"), { target: { value: "abc" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("secrets.KEY: too short")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.getByText("Set")).toBeTruthy());
    expect(screen.queryByDisplayValue("abc")).toBeNull();
    expect(screen.getByRole("button", { name: "Replace" })).toBeTruthy();
  });
});
