import { beforeEach, describe, expect, it } from "vitest";
import { takeSetupToken } from "./setup-token";

describe("takeSetupToken", () => {
  beforeEach(() => window.history.replaceState(null, "", "/"));

  it("reads the fragment and removes it from the address bar", () => {
    window.history.replaceState(null, "", "/setup#token=abc123&x=1");
    expect(takeSetupToken()).toBe("abc123");
    expect(window.location.hash).toBe("");
    expect(window.location.search).toBe("");
    expect(window.location.pathname).toBe("/setup");
  });

  it("ignores a query-string token", () => {
    window.history.replaceState(null, "", "/setup?token=leaked");
    expect(takeSetupToken()).toBeNull();
    expect(window.location.search).toBe("");
  });

  it("returns null for an empty token", () => {
    window.history.replaceState(null, "", "/setup#token=");
    expect(takeSetupToken()).toBeNull();
  });
});
