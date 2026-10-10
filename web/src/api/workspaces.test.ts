import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, verb } from "./request";
import { launchIn } from "./workspaces";

vi.mock("./request", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./request")>()),
  verb: vi.fn(),
}));
vi.mock("@/lib/notify", () => ({ notify: vi.fn() }));

describe("launchIn", () => {
  beforeEach(() => {
    vi.mocked(verb).mockReset();
  });

  it("launches an agent, or a shell without one", async () => {
    vi.mocked(verb).mockResolvedValue({ session: "ws-api", window: "claude" });
    expect(await launchIn("api", "@anthropic-ai/claude-code")).toBe(true);
    expect(verb).toHaveBeenCalledWith("workspaces.launch", {
      id: "api",
      agent: "@anthropic-ai/claude-code",
    });
    expect(await launchIn("api")).toBe(true);
    expect(verb).toHaveBeenLastCalledWith("workspaces.launch", { id: "api" });
  });

  it("says why a launch failed", async () => {
    const { notify } = await import("@/lib/notify");
    vi.mocked(verb).mockRejectedValue(
      new ApiError(409, {
        error: {
          code: "agent_disabled",
          message: "Claude Code is switched off; turn it on on the Agents page",
        },
      }),
    );
    expect(await launchIn("api", "@anthropic-ai/claude-code")).toBe(false);
    expect(notify).toHaveBeenCalledWith(
      "Claude Code is switched off; turn it on on the Agents page",
      "bad",
    );
  });
});
