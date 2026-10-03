import { describe, expect, it } from "vitest";
import { ApiError } from "@/api/request";
import { lockSeconds, strength, USER } from "./auth-logic";

describe("auth logic", () => {
  it("rates passwords like M3", () => {
    expect(strength("short")).toBe("Too short: use at least 10 characters.");
    expect(strength("abcdefghij")).toBe("Acceptable. A longer passphrase is stronger.");
    expect(strength("abcdefghiJ1")).toBe("Strong.");
    expect(strength("a".repeat(16))).toBe("Strong.");
  });

  it("accepts the same user names as the server", () => {
    expect(USER.test("dev.user@example")).toBe(true);
    expect(USER.test("has space")).toBe(false);
    expect(USER.test("x".repeat(65))).toBe(false);
  });

  it("locks sign-in after throttling", () => {
    expect(
      lockSeconds(new ApiError(429, { error: { code: "too_many_attempts", retry_after: 7 } })),
    ).toBe(7);
    expect(lockSeconds(new ApiError(429, null))).toBe(1);
    expect(
      lockSeconds(new ApiError(401, { error: { code: "invalid_login", retry_after: 1 } })),
    ).toBe(0);
    expect(
      lockSeconds(new ApiError(401, { error: { code: "invalid_login", retry_after: 4 } })),
    ).toBe(4);
    expect(lockSeconds(new Error("x"))).toBe(0);
  });
});
