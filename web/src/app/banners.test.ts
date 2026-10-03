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

  it("offers an update everywhere but on the agents page", () => {
    const check = { ref: "main", hash: "9e8d7c6b5a", update_available: true, checked_at: "" };
    expect(updateBanner(status({ check }), "/")).toEqual(check);
    expect(updateBanner(status({ check }), "/agents")).toBeNull();
    expect(updateBanner(status({ check: { ...check, update_available: false } }), "/")).toBeNull();
    expect(updateBanner(undefined, "/")).toBeNull();
  });
});
