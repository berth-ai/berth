import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useConfirm, type ConfirmSpec } from "./confirm";

function Harness({ spec }: { spec: ConfirmSpec }) {
  const [dialog, confirm] = useConfirm();
  return (
    <>
      <button type="button" onClick={() => confirm(spec)}>
        open
      </button>
      {dialog}
    </>
  );
}

describe("useConfirm", () => {
  it("stays open when the action returns false and closes when it succeeds", async () => {
    const action = vi
      .fn<() => Promise<boolean>>()
      .mockResolvedValueOnce(false)
      .mockResolvedValueOnce(true);
    render(
      <Harness
        spec={{
          title: "Roll back?",
          body: "Berth switches back.",
          confirm: "Roll back",
          danger: true,
          action,
        }}
      />,
    );
    fireEvent.click(screen.getByText("open"));
    fireEvent.click(await screen.findByRole("button", { name: "Roll back" }));
    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    expect(screen.getByText("Roll back?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Roll back" }));
    await waitFor(() => expect(screen.queryByText("Roll back?")).toBeNull());
  });
});
