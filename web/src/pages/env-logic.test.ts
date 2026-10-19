import { describe, expect, it } from "vitest";
import type { Workspace } from "@/api/types";
import {
  envKeyError,
  envRows,
  envValueError,
  removePatch,
  secretName,
  setPlainPatch,
  setSecretPatch,
} from "./env-logic";

const ws: Workspace = {
  path: "acme/api",
  repo: "",
  env: {
    LOG: { value: "debug", secret: false },
    KEY: { value: "", secret: true },
    ZED: { value: "", secret: true },
  },
};

describe("env", () => {
  it("rows are sorted, secrets with their state", () => {
    expect(envRows("api", ws, { "workspaces.api.KEY": { set: true } })).toEqual([
      { key: "KEY", secret: true, value: "", set: true },
      { key: "LOG", secret: false, value: "debug", set: true },
      { key: "ZED", secret: true, value: "", set: false },
    ]);
  });
  it("mirrors the key rules", () => {
    expect(envKeyError("STRIPE_KEY", [])).toBe("");
    expect(envKeyError("lower", [])).toMatch(/capital letters/);
    expect(envKeyError("PATH", [])).toMatch(/reserved/);
    expect(envKeyError("GIT_DIR", [])).toMatch(/reserved/);
    expect(envKeyError("LOG", ["LOG"])).toMatch(/exists/);
    expect(envKeyError("A".repeat(129), [])).toMatch(/capital letters/);
  });
  it("mirrors the value rules", () => {
    expect(envValueError("multi\nline")).toBe("");
    expect(envValueError("a\u0000b")).toMatch(/NUL/);
    expect(envValueError("é".repeat(2049))).toMatch(/4 KiB/);
  });
  it("builds patches and names", () => {
    expect(secretName("api", "KEY")).toBe("workspaces.api.KEY");
    expect(setPlainPatch("api", "LOG", "x")).toEqual({
      workspaces: { api: { env: { LOG: { value: "x", secret: false } } } },
    });
    expect(setSecretPatch("api", "KEY")).toEqual({
      workspaces: { api: { env: { KEY: { value: "", secret: true } } } },
    });
    expect(removePatch("api", "KEY")).toEqual({ workspaces: { api: { env: { KEY: null } } } });
  });
});
