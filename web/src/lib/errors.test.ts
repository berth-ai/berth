import { describe, expect, it } from "vitest";
import { ApiError } from "@/api/request";
import { errorText, fieldErrors } from "./errors";

describe("errors", () => {
  it("prefers field errors, then the message, then the code", () => {
    const fields = new ApiError(400, {
      error: {
        code: "invalid_config",
        message: "x",
        fields: [{ field: "runtime.tz", message: "set a zone" }],
      },
    });
    expect(errorText(fields)).toBe("runtime.tz: set a zone");
    expect(fieldErrors(fields)).toEqual({ "runtime.tz": "set a zone" });
    expect(errorText(new ApiError(409, { error: { code: "busy", message: "wait" } }))).toBe("wait");
    expect(errorText(new ApiError(409, { error: { code: "busy" } }))).toBe("busy");
  });

  it("falls back when the server said nothing useful", () => {
    expect(errorText(new ApiError(502, null))).toBe("The panel answered 502.");
    expect(errorText(new ApiError(502, null), "The job did not start.")).toBe(
      "The job did not start.",
    );
    expect(errorText(new Error("boom"))).toBe("The request failed.");
  });
});
