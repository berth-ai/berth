import { beforeEach, describe, expect, it, vi } from "vitest";
import { replaceLocation } from "@/lib/nav";
import { ApiError, getOnline, post, request, shouldRetry, subscribeOnline, verb } from "./request";

vi.mock("@/lib/nav", () => ({ replaceLocation: vi.fn() }));

function answer(status: number, body: unknown) {
  return vi.fn(
    async () => new Response(body === undefined ? "" : JSON.stringify(body), { status }),
  );
}

describe("request", () => {
  beforeEach(() => {
    vi.mocked(replaceLocation).mockClear();
    window.history.replaceState(null, "", "/");
  });

  it("posts JSON with the CSRF header and no caching", async () => {
    const fetch = answer(200, { ok: true });
    vi.stubGlobal("fetch", fetch);
    await expect(verb("config.get")).resolves.toEqual({ ok: true });
    expect(fetch).toHaveBeenCalledWith("/api/verbs/config.get", {
      cache: "no-store",
      method: "POST",
      body: "{}",
      headers: { "Content-Type": "application/json", "X-Berth-Panel": "1" },
    });
  });

  it("sends a lost session to sign-in once and throws", async () => {
    vi.stubGlobal(
      "fetch",
      answer(401, { error: { code: "unauthorized", message: "sign in first" } }),
    );
    const err = await request("/api/status").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(replaceLocation).toHaveBeenCalledWith("/login");
  });

  it("does not redirect a failed sign-in", async () => {
    vi.stubGlobal(
      "fetch",
      answer(401, { error: { code: "invalid_login", message: "x", retry_after: 1 } }),
    );
    const err = await post("/api/login", {}).catch((e: unknown) => e);
    expect((err as ApiError).retryAfter).toBe(1);
    expect(replaceLocation).not.toHaveBeenCalled();
  });

  it("sends an unfinished setup to the wizard", async () => {
    vi.stubGlobal("fetch", answer(409, { error: { code: "setup_required", message: "x" } }));
    await request("/api/status").catch(() => undefined);
    expect(replaceLocation).toHaveBeenCalledWith("/setup");
  });

  it("tracks reachability and notifies watchers", async () => {
    const seen: boolean[] = [];
    const off = subscribeOnline(() => seen.push(getOnline()));
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("Failed to fetch");
      }),
    );
    const err = await request("/api/status").catch((e: unknown) => e);
    expect((err as ApiError).status).toBe(0);
    expect((err as ApiError).code).toBe("network");
    vi.stubGlobal("fetch", answer(200, {}));
    await request("/api/status");
    off();
    expect(seen).toEqual([false, true]);
  });

  it("never retries client errors", () => {
    expect(shouldRetry(0, new ApiError(401, null))).toBe(false);
    expect(shouldRetry(0, new ApiError(404, null))).toBe(false);
    expect(shouldRetry(0, new ApiError(0, null))).toBe(true);
    expect(shouldRetry(1, new ApiError(502, null))).toBe(true);
    expect(shouldRetry(2, new ApiError(502, null))).toBe(false);
  });
});
