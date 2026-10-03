import { beforeEach, describe, expect, it, vi } from "vitest";
import { reloadOnSetupToken, takeSetupToken } from "./setup-token";

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

describe("reloadOnSetupToken", () => {
  beforeEach(() => window.history.replaceState(null, "", "/setup"));

  // A link pasted into a tab already on /setup changes only the fragment, so
  // the page would keep the token in the address bar and never read it.
  it("reloads when a token arrives in the fragment", () => {
    const reload = vi.fn();
    const stop = reloadOnSetupToken(reload);
    window.history.replaceState(null, "", "/setup#token=abc123");
    window.dispatchEvent(new HashChangeEvent("hashchange"));
    expect(reload).toHaveBeenCalledOnce();
    stop();
  });

  it("ignores fragments without a token", () => {
    const reload = vi.fn();
    const stop = reloadOnSetupToken(reload);
    window.history.replaceState(null, "", "/setup#other");
    window.dispatchEvent(new HashChangeEvent("hashchange"));
    expect(reload).not.toHaveBeenCalled();
    stop();
  });
});
