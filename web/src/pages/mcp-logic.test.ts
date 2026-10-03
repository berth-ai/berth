import { describe, expect, it } from "vitest";
import type { AgentPackage, Config } from "@/api/types";
import {
  availableLabel,
  idError,
  mcpRows,
  pickedAfter,
  removePatch,
  serverPatch,
  tokenName,
  urlError,
} from "./mcp-logic";

const pkg = (p: Partial<AgentPackage>): AgentPackage => ({
  package: "x",
  name: "X",
  kind: "agent",
  version: "1",
  enabled: true,
  bins: [],
  keys: [],
  ...p,
});
const agents = [
  pkg({ package: "@a/claude", name: "Claude Code" }),
  pkg({ package: "codex", name: "Codex" }),
];
const files = pkg({ package: "@x/files", name: "Files MCP", kind: "mcp", mcp_id: "files" });

describe("ids and urls", () => {
  it("names tokens", () => expect(tokenName("my-server")).toBe("MCP_MY_SERVER_TOKEN"));
  it("checks ids", () => {
    expect(idError("", [])).toBe("Enter an id.");
    expect(idError("Bad_ID", [])).toMatch(/lowercase/);
    expect(idError("a".repeat(49), [])).toMatch(/lowercase/);
    expect(idError("files", ["files"])).toBe("files is already in use.");
    expect(idError("linear", ["files"])).toBe("");
  });
  it("checks urls", () => {
    expect(urlError("")).toBe("Enter the server's URL.");
    expect(urlError("mcp.example.com")).toMatch(/absolute URL/);
    expect(urlError("http://mcp.example.com/mcp")).toBe("Use https unless the host is localhost.");
    expect(urlError("http://localhost:8080/mcp")).toBe("");
    expect(urlError("http://127.0.0.1/mcp")).toBe("");
    expect(urlError("http://[::1]:9000/mcp")).toBe("");
    expect(urlError("https://u:p@mcp.example.com")).toMatch(/user name/);
    expect(urlError("ftp://mcp.example.com")).toBe("Start the URL with https://.");
    expect(urlError("https://mcp.example.com/mcp")).toBe("");
  });
});

describe("rows and labels", () => {
  const cfg = {
    mcp: {
      servers: {
        linear: { url: "https://l.example/mcp", auth: "bearer", enabled: true },
        files: { url: "https://f.example/mcp", auth: "none", enabled: true },
      },
      available: {},
    },
  } as unknown as Config;
  it("lists lock servers, then user servers, and flags clashes and tokens", () => {
    const rows = mcpRows([...agents, files], cfg, { MCP_LINEAR_TOKEN: { set: true } });
    expect(rows.map((r) => [r.kind, r.id])).toEqual([
      ["lock", "files"],
      ["user", "files"],
      ["user", "linear"],
    ]);
    expect(rows[1]).toMatchObject({ clash: true });
    expect(rows[2]).toMatchObject({ clash: false, tokenSet: true });
  });
  it("copes with a broker that has no mcp section", () => {
    expect(mcpRows([files], {} as Config, {}).map((r) => r.id)).toEqual(["files"]);
  });
  it("labels who gets a server", () => {
    expect(availableLabel(undefined, agents)).toBe("All agents");
    expect(availableLabel(["codex"], agents)).toBe("Codex");
    expect(availableLabel(["gone"], agents)).toBe("No installed agent");
  });
  it("collapses a full pick to all agents and never empties it", () => {
    const all = agents.map((a) => a.package);
    expect(pickedAfter(null, all, "codex", false)).toEqual(["@a/claude"]);
    expect(pickedAfter(["@a/claude"], all, "codex", true)).toBeNull();
    expect(pickedAfter(["@a/claude"], all, "@a/claude", false)).toEqual(["@a/claude"]);
  });
});

describe("patches", () => {
  it("adds and removes", () => {
    expect(serverPatch("l", { url: "u", auth: "none", enabled: true }, null)).toEqual({
      mcp: { servers: { l: { url: "u", auth: "none", enabled: true } }, available: { l: null } },
    });
    expect(removePatch("l")).toEqual({ mcp: { servers: { l: null }, available: { l: null } } });
  });
});
