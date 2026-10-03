import { describe, expect, it } from "vitest";
import type { Job } from "./types";
import { jobCommand, jobResultText, jobsInterval, jobTitle } from "./jobs";

const job = (over: Partial<Job>): Job => ({
  id: 1,
  verb: "update",
  group: "agents",
  args: ["update"],
  state: "running",
  started_at: "2026-10-03T12:00:00Z",
  finished_at: null,
  exit_code: null,
  result: null,
  log_truncated: false,
  ...over,
});

describe("jobs", () => {
  it("titles jobs like M3", () => {
    expect(jobTitle(job({}))).toBe("Agent update");
    expect(jobTitle(job({ verb: "forge.login", args: ["/usr/bin/glab", "auth", "login"] }))).toBe(
      "Sign in to GitLab",
    );
    expect(jobTitle(job({ verb: "forge.login", args: ["/usr/bin/gh", "auth", "login"] }))).toBe(
      "Sign in to GitHub",
    );
    expect(jobTitle(job({ verb: "mystery" }))).toBe("mystery");
  });

  it("shows the command as the operator would type it", () => {
    expect(jobCommand(job({ args: ["update", "--image"] }))).toBe("berth update --image");
    expect(
      jobCommand(job({ group: "tools", args: ["/opt/mise/bin/mise", "use", "-g", "node@24"] })),
    ).toBe("mise use -g node@24");
  });

  it("polls fast only while a job runs", () => {
    expect(jobsInterval([job({ state: "succeeded" })])).toBe(5000);
    expect(jobsInterval([job({ state: "succeeded" }), job({ state: "running" })])).toBe(1000);
    expect(jobsInterval(undefined)).toBe(5000);
  });

  it("explains a failed result", () => {
    expect(
      jobResultText(job({ result: { error: { code: "x", step: "switch", message: "no set" } } })),
    ).toBe("switch: no set");
    expect(jobResultText(job({ result: { error: { code: "x" } } }))).toBe("x");
    expect(jobResultText(job({ result: {} }))).toBeNull();
  });
});
