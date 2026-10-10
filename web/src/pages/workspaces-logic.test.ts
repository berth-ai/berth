import { describe, expect, it } from "vitest";
import type { AgentPackage, Config, WorkspaceState } from "@/api/types";
import {
  defaultPath,
  deriveId,
  folderError,
  gitSummary,
  idError,
  launchAgents,
  parseWorkspaceTab,
  terminalPath,
  workspaceRows,
} from "./workspaces-logic";

const cfg = {
  workspaces: { web: { path: "acme/web", repo: "" }, api: { path: "acme/api", repo: "" } },
} as unknown as Config;

const state = (s: Partial<WorkspaceState> = {}): WorkspaceState => ({
  exists: true,
  writable: true,
  git: true,
  branch: "main",
  head: "0123456789ab",
  dirty: 0,
  session: false,
  windows: [],
  unknown: false,
  ...s,
});

const pkg = (p: Partial<AgentPackage>): AgentPackage =>
  ({
    package: "x",
    name: "X",
    kind: "agent",
    version: "1",
    enabled: true,
    bins: ["x"],
    keys: [],
    ...p,
  }) as AgentPackage;

describe("workspaceRows", () => {
  it("sorts by id and tolerates a config without workspaces", () => {
    expect(workspaceRows(cfg).map((r) => r.id)).toEqual(["api", "web"]);
    expect(workspaceRows(undefined)).toEqual([]);
    expect(workspaceRows({} as Config)).toEqual([]);
  });
});

describe("defaultPath", () => {
  it("is owner/repo of the URL", () => {
    expect(defaultPath("https://github.com/acme/api")).toBe("acme/api");
    expect(defaultPath("https://github.com/acme/api/")).toBe("acme/api");
    expect(defaultPath("https://gitlab.com/group/sub/tool.git")).toBe("sub/tool");
    expect(defaultPath("https://example.com/solo")).toBe("solo");
    expect(defaultPath("not a url")).toBe("");
  });
});

describe("deriveId", () => {
  it("turns a folder into an id", () => {
    expect(deriveId("acme/api")).toBe("acme-api");
    expect(deriveId("Octocat/Hello_World")).toBe("octocat-hello-world");
    expect(deriveId("--x--")).toBe("x");
    expect(deriveId(`${"a".repeat(63)}/b`)).toBe("a".repeat(63));
    expect(deriveId("")).toBe("");
  });
});

describe("idError", () => {
  it("mirrors the id rule and refuses a taken id", () => {
    expect(idError("", cfg)).not.toBe("");
    expect(idError("Api", cfg)).not.toBe("");
    expect(idError("-api", cfg)).not.toBe("");
    expect(idError("api", cfg)).toBe("A workspace named api exists; choose another id.");
    expect(idError("docs", cfg)).toBe("");
  });
});

describe("folderError", () => {
  it("mirrors the path rules and refuses taken or nested folders", () => {
    for (const bad of ["", "/acme", "acme/../x", "acme/", "acme/.git", "acme/.berth-tmp-1/x"])
      expect(folderError(bad, cfg), bad).not.toBe("");
    expect(folderError("acme/api", cfg)).toBe("Workspace api already uses this folder.");
    expect(folderError("acme", cfg)).toBe(
      "This folder overlaps workspace api (acme/api); workspaces cannot nest.",
    );
    expect(folderError("acme/api/sub", cfg)).toContain("overlaps workspace api");
    expect(folderError("acme/api2", cfg)).toBe("");
  });
});

describe("gitSummary", () => {
  it("says what the list shows", () => {
    expect(gitSummary(undefined)).toBe("Unknown");
    expect(gitSummary(state({ unknown: true }))).toBe("Unknown");
    expect(gitSummary(state({ exists: false }))).toBe("Folder missing");
    expect(gitSummary(state({ git: false }))).toBe("Not a git repository");
    expect(gitSummary(state())).toBe("main");
    expect(gitSummary(state({ dirty: 1 }))).toBe("main · 1 change");
    expect(gitSummary(state({ dirty: 3 }))).toBe("main · 3 changes");
    expect(gitSummary(state({ branch: "" }))).toBe("detached at 0123456");
    expect(gitSummary(state({ branch: "", head: "" }))).toBe("no commits");
  });
});

describe("launchAgents", () => {
  it("keeps enabled agents that have a program", () => {
    const agents = launchAgents([
      pkg({ package: "a" }),
      pkg({ package: "off", enabled: false }),
      pkg({ package: "mcp", kind: "mcp" }),
      pkg({ package: "nobin", bins: [] }),
      pkg({ package: "broken", error: "Not installed yet" }),
    ]);
    expect(agents.map((a) => a.package)).toEqual(["a"]);
  });
});

describe("terminal", () => {
  it("names the session and the tab", () => {
    expect(terminalPath("api")).toBe("/terminal/?arg=ws-api");
    expect(parseWorkspaceTab("terminal")).toBe("terminal");
    expect(parseWorkspaceTab("bogus")).toBe("overview");
    expect(parseWorkspaceTab(undefined)).toBe("overview");
  });
});
