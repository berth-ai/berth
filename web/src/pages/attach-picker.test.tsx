import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentPackage, Attachment } from "@/api/types";
import { AttachPicker } from "./attach-picker";

const agent = (pkg: string, name: string, via?: string[]): AgentPackage => ({
  package: pkg,
  name,
  kind: "agent",
  version: "1",
  enabled: true,
  bins: [],
  keys: [],
  skills_dir: `~/.${pkg}/skills`,
  skills_via: via,
});
const AGENTS = [agent("a", "Alpha"), agent("b", "Beta", ["a"]), agent("c", "Gamma")];

const box = (name: RegExp) => screen.getByRole("checkbox", { name });
const button = (name: string) => screen.getByRole<HTMLButtonElement>("button", { name });

function picker(props: { attachment?: Attachment | null; taken?: Record<string, string> } = {}) {
  const onSave = vi.fn(async () => true);
  const onClose = vi.fn();
  render(
    <AttachPicker
      name="notes"
      source="acme-eng-skills"
      attachment={props.attachment ?? null}
      agents={AGENTS}
      unsupported={[agent("x", "Xeno")]}
      taken={props.taken ?? {}}
      onSave={onSave}
      onClose={onClose}
    />,
  );
  return { onSave, onClose };
}

describe("AttachPicker", () => {
  it("attaches to every agent, including later ones, by default", async () => {
    const { onSave, onClose } = picker();
    expect(box(/^All agents, including ones added later/).getAttribute("aria-checked")).toBe(
      "true",
    );
    expect(box(/^Xeno/).hasAttribute("data-disabled")).toBe(true);
    fireEvent.click(button("Attach"));
    await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onSave).toHaveBeenCalledWith(null);
  });

  it("lists today's agents once All is unticked", async () => {
    const { onSave } = picker();
    fireEvent.click(box(/^All agents/));
    fireEvent.click(box(/^Gamma/));
    fireEvent.click(button("Attach"));
    await vi.waitFor(() => expect(onSave).toHaveBeenCalledWith(["a", "b"]));
  });

  it("locks the agents another source provides", () => {
    picker({ taken: { a: "other", b: "other" } });
    expect(box(/^All agents/).hasAttribute("data-disabled")).toBe(true);
    expect(box(/^Alpha/).hasAttribute("data-disabled")).toBe(true);
    expect(box(/^Alpha/).getAttribute("aria-checked")).toBe("false");
    expect(screen.getAllByText("Gets notes from other")).toHaveLength(2);
    expect(box(/^Gamma/).getAttribute("aria-checked")).toBe("true");
  });

  it("shows an agent reached through another's folder as ticked and locked", () => {
    picker({ attachment: { enabled: true, agents: ["a"] } });
    expect(box(/^Beta/).getAttribute("aria-checked")).toBe("true");
    expect(box(/^Beta/).hasAttribute("data-disabled")).toBe(true);
    expect(screen.getByText("Through Alpha")).toBeTruthy();
  });

  it("turns Save into Detach once no agent is left", async () => {
    const { onSave } = picker({ attachment: { enabled: true, agents: ["c"] } });
    expect(button("Save")).toBeTruthy();
    fireEvent.click(box(/^Gamma/));
    fireEvent.click(button("Detach"));
    await vi.waitFor(() => expect(onSave).toHaveBeenCalledWith([]));
  });

  it("cannot attach to no agent", () => {
    picker({ taken: { "*": "o", a: "o", b: "o", c: "o" } });
    expect(button("Attach").disabled).toBe(true);
  });
});
