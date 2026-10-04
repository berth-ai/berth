import { describe, expect, it } from "vitest";
import type { Job, LoginState } from "@/api/types";
import { cleanCode, dialogStep, methodLines, runningLogin } from "./login-logic";

const login = (over: Partial<LoginState>): LoginState => ({
  agent: "@a/cli",
  method: "m",
  flow: "paste",
  url: "",
  code: "",
  waiting: false,
  inputs: 0,
  signed_in: false,
  account: "",
  failed: "",
  ...over,
});

const job = (over: Partial<Job>): Job => ({
  id: 7,
  verb: "agent.login",
  group: "agent-login",
  args: ["/usr/local/bin/berth", "login", "run", "@a/cli", "m"],
  state: "running",
  started_at: "",
  finished_at: null,
  exit_code: null,
  result: null,
  log_truncated: false,
  ...over,
});

describe("sign-in logic", () => {
  it("cleans a pasted code", () => {
    expect(cleanCode("  abc#1\n")).toBe("abc#1");
    expect(cleanCode("   ")).toBeNull();
    expect(cleanCode("a\nb")).toBeNull();
    expect(cleanCode("x".repeat(2049))).toBeNull();
    expect(cleanCode("é".repeat(1025))).toBeNull(); // 2050 bytes
  });

  it("finds a running sign-in to resume", () => {
    expect(runningLogin([job({})])).toEqual({ id: 7, agent: "@a/cli", method: "m" });
    expect(runningLogin([job({ state: "failed" }), job({ verb: "forge.login" })])).toBeNull();
    expect(runningLogin(undefined)).toBeNull();
  });

  it("turns a job into the dialog's step", () => {
    expect(dialogStep(undefined)).toEqual({ kind: "starting" });
    expect(dialogStep(job({ login: login({ flow: "device", url: "https://d.example" }) }))).toEqual(
      {
        kind: "starting",
      },
    );
    expect(
      dialogStep(
        job({ login: login({ flow: "device", url: "https://d.example", code: "AB12-CD34" }) }),
      ),
    ).toEqual({ kind: "device", url: "https://d.example", code: "AB12-CD34" });
    expect(dialogStep(job({ login: login({ url: "https://p.example", waiting: true }) }))).toEqual({
      kind: "paste",
      url: "https://p.example",
      waiting: true,
      retry: false,
    });
    expect(
      dialogStep(job({ login: login({ url: "https://p.example/2", waiting: true, inputs: 1 }) })),
    ).toMatchObject({ kind: "paste", retry: true });
    expect(
      dialogStep(
        job({ state: "succeeded", login: login({ signed_in: true, account: "you@example.org" }) }),
      ),
    ).toEqual({ kind: "done", account: "you@example.org" });
    expect(
      dialogStep(
        job({ state: "failed", login: login({ inputs: 1, failed: "cli exited with status 1" }) }),
      ),
    ).toEqual({
      kind: "failed",
      message: "cli exited with status 1",
      badCode: true,
      cancelled: false,
    });
    expect(
      dialogStep(
        job({ state: "failed", result: { error: { code: "cancelled", message: "cancelled" } } }),
      ),
    ).toMatchObject({ kind: "failed", cancelled: true, badCode: false });
  });

  it("lists a status's methods as lines", () => {
    const one = [
      { id: "c", label: "ChatGPT", flow: "device", signed_in: true, account: "", error: "" },
    ];
    expect(methodLines(one)).toEqual([
      { id: "c", label: null, flow: "device", state: "signed_in", account: "", error: "" },
    ]);
    const two = [
      {
        id: "c",
        label: "ChatGPT",
        flow: "device",
        signed_in: true,
        account: "you@example.org",
        error: "",
      },
      {
        id: "g",
        label: "GitHub Copilot",
        flow: "device",
        signed_in: false,
        account: "",
        error: "timed out",
      },
    ];
    expect(methodLines(two).map((l) => [l.label, l.state, l.account])).toEqual([
      ["ChatGPT", "signed_in", "you@example.org"],
      ["GitHub Copilot", "unknown", ""],
    ]);
    expect(methodLines(null)).toEqual([]);
  });
});
