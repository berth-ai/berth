import { describe, expect, it } from "vitest";
import type { ForgeStatus, Job } from "@/api/types";
import { deviceHost, forgeJobId, hostError, identityHosts, tokenPage } from "./accounts-logic";

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

describe("host identities", () => {
  it("validates a host like the broker", () => {
    expect(hostError("", [])).toBe("");
    expect(hostError("GitLab.com", [])).toBe("");
    expect(hostError("https://git.example.org:3000/x", [])).toBe("");
    for (const bad of ["-x.org", "a b", "a..b", "a_b.org", "a:b"]) {
      expect(hostError(bad, [])).toMatch(/host name/);
    }
    expect(hostError("gitlab.com", ["gitlab.com"])).toMatch(/already/);
  });

  it("offers signed-in hosts without an identity", () => {
    const data: ForgeStatus = {
      git: { name: "", email: "" },
      identities: [{ host: "github.com", name: "A", email: "a@example.org" }],
      forges: {
        gh: { host: "github.com", signed_in: true, user: "" },
        glab: { host: "gitlab.com", signed_in: false, user: "" },
        tea: {
          host: "codeberg.org",
          signed_in: true,
          user: "",
          logins: [
            { name: "codeberg.org", url: "https://codeberg.org", user: "", default: true },
            { name: "git", url: "https://git.example.org:3000/sub", user: "", default: false },
          ],
        },
      },
    };
    expect(identityHosts(data)).toEqual(["codeberg.org", "git.example.org:3000"]);
  });
});
