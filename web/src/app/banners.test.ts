import { describe, expect, it } from "vitest";
import type { Status } from "@/api/types";
import { failedCount, updateBanner } from "./banners";

const status = (over: Partial<Status>): Status => ({
  mode: "normal",
  revision: 1,
  services: [],
  agents: null,
  packages: [],
  check: null,
  host: null,
  ...over,
});

describe("banners", () => {
  it("counts failed services for the nav", () => {
    const st = status({
      services: [
        { name: "panel", state: "running", pid: 1, started_at: null, restarts: 0, log: null },
        { name: "code_server", state: "failed", pid: 0, started_at: null, restarts: 3, log: null },
      ],
    });
    expect(failedCount(st)).toBe(1);
    expect(failedCount(undefined)).toBe(0);
  });

  it("offers minor and patch updates everywhere but on the updates page", () => {
    const check = { checked_at: "", pins: [], updates: 2, majors: 1, update_available: true };
    expect(updateBanner(status({ check }), "/")).toBe(
      "2 newer versions are available for your agents and tools.",
    );
    expect(updateBanner(status({ check }), "/updates")).toBeNull();
    expect(updateBanner(status({ check: { ...check, updates: 0 } }), "/")).toBeNull();
    expect(updateBanner(undefined, "/")).toBeNull();
  });
});
