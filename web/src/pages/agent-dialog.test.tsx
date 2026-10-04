import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Job, RegistryAgent } from "@/api/types";
import { AddAgentDialog } from "./agent-dialog";

vi.mock("@/api/run-job", () => ({ runJob: vi.fn() }));

const copilot: RegistryAgent = {
  id: "copilot",
  package: "@github/copilot",
  name: "GitHub Copilot CLI",
  description: "GitHub's coding agent, signed in with your GitHub account",
  version: "1.0.91",
  icon: "githubcopilot",
  keys: ["COPILOT_GITHUB_TOKEN"],
  login: ["GitHub"],
  added: false,
};

const job = (state: string, result: Job["result"], log = ""): Job => ({
  id: 1,
  verb: "agents.add",
  group: "agents",
  args: [],
  state,
  started_at: "",
  finished_at: "",
  exit_code: state === "succeeded" ? 0 : 1,
  log_truncated: false,
  result,
  log,
});

const dialog = (props: Partial<Parameters<typeof AddAgentDialog>[0]> = {}) =>
  render(
    <AddAgentDialog
      entries={[copilot]}
      failed=""
      onClose={() => {}}
      onAdded={() => {}}
      {...props}
    />,
  );

describe("AddAgentDialog", () => {
  beforeEach(async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockReset();
  });

  it("shows each entry's version, sign-in methods and keys", async () => {
    dialog();
    expect(await screen.findByText("GitHub Copilot CLI")).toBeTruthy();
    expect(screen.getByText("1.0.91")).toBeTruthy();
    expect(screen.getByText(/Sign-in: GitHub/)).toBeTruthy();
    expect(screen.getByText("COPILOT_GITHUB_TOKEN")).toBeTruthy();
  });

  it("disables an added entry and badges it", async () => {
    dialog({ entries: [{ ...copilot, added: true }] });
    expect(await screen.findByText("Added")).toBeTruthy();
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Add GitHub Copilot CLI" }).disabled,
    ).toBe(true);
  });

  it("adds an entry and closes", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue(job("succeeded", {}));
    const onClose = vi.fn();
    const onAdded = vi.fn();
    dialog({ onClose, onAdded });
    fireEvent.click(await screen.findByRole("button", { name: "Add GitHub Copilot CLI" }));
    await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onAdded).toHaveBeenCalledWith("GitHub Copilot CLI");
    expect(runJob).toHaveBeenCalledWith("agents.add", { id: "copilot" }, expect.any(Function));
  });

  it("keeps the error and the log on a failed install", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockImplementation(async (_verb, _body, onUpdate) => {
      const failed = job(
        "failed",
        { error: { code: "install_failed", message: "npm ci failed" } },
        "npm ERR! network down\n",
      );
      onUpdate?.(failed);
      return failed;
    });
    const onClose = vi.fn();
    dialog({ onClose });
    fireEvent.click(await screen.findByRole("button", { name: "Add GitHub Copilot CLI" }));
    expect(await screen.findByText("npm ci failed")).toBeTruthy();
    expect(screen.getByText("npm ERR! network down")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("says when the registry did not load", async () => {
    dialog({ entries: undefined, failed: "The supervisor is not reachable." });
    expect(await screen.findByText("The supervisor is not reachable.")).toBeTruthy();
  });
});
