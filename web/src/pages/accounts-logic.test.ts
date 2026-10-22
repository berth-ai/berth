import { describe, expect, it } from "vitest";
import type { Job } from "@/api/types";
import { deviceHost, forgeJobId, tokenPage } from "./accounts-logic";

const job = (id: number, verb: string, state: string, args: string[]): Job => ({
  id,
  verb,
  group: "forge",
  args,
  state,
  started_at: "",
  finished_at: null,
  exit_code: null,
  result: null,
  log_truncated: false,
});

describe("accounts logic", () => {
  it("shows the device URL without scheme or query", () => {
    expect(deviceHost("https://github.com/login/device?x=1")).toBe("github.com/login/device");
  });

  it("finds the running sign-in for one forge", () => {
    const jobs = [
      job(3, "forge.login", "running", ["/usr/bin/glab", "auth"]),
      job(2, "forge.login", "succeeded", ["/usr/bin/gh", "auth"]),
    ];
    expect(forgeJobId(jobs, "glab")).toBe(3);
    expect(forgeJobId(jobs, "gh")).toBeNull();
    expect(forgeJobId(undefined, "gh")).toBeNull();
  });

  it("links a server's token page once its address is https", () => {
    expect(tokenPage(" https://gitea.com/ ")).toBe("https://gitea.com/user/settings/applications");
    expect(tokenPage("https://example.org/git")).toBe(
      "https://example.org/git/user/settings/applications",
    );
    expect(tokenPage("http://gitea.com")).toBeNull();
    expect(tokenPage("gitea.com")).toBeNull();
    expect(tokenPage("")).toBeNull();
  });
});
