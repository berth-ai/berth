import { describe, expect, it } from "vitest";
import type { Config } from "@/api/types";
import { PATHS, exportedConfig, predict, readImport } from "./settings-logic";

const base: Config = {
  schema: 1,
  revision: 4,
  frontends: { terminal: { enabled: true }, ide: { enabled: false } },
  agents: { ref: "main", disabled: [], keep: 2, boot_timeout: 600, mcp_timeout: 300 },
  runtime: { tz: "Etc/UTC" },
};

function edit(from: Config, change: (c: Config) => void): Config {
  const c = structuredClone(from);
  change(c);
  return c;
}

describe("PATHS", () => {
  it("tracks the Orca settings", () => {
    expect(PATHS).toContain("frontends.orca.enabled");
    expect(PATHS).toContain("frontends.orca.pairing_address");
  });
});

describe("predict for Orca", () => {
  const withOrca = edit(base, (c) => {
    c.frontends.orca = { enabled: false, pairing_address: "" };
  });
  it("says Orca starts, stops and restarts", () => {
    const on = edit(withOrca, (c) => {
      c.frontends.orca!.enabled = true;
    });
    expect(predict(withOrca, on)).toBe("Starts Orca.");
    expect(predict(on, withOrca)).toBe("Stops Orca.");
    expect(
      predict(
        on,
        edit(on, (c) => {
          c.runtime.tz = "Europe/Rome";
        }),
      ),
    ).toBe("Restarts the terminal and Orca; your terminal sessions keep running.");
  });
  it("reads a config without Orca as off", () => {
    expect(predict(base, withOrca)).toBe("Nothing restarts.");
  });
});

describe("predict", () => {
  it("says what a save restarts", () => {
    expect(predict(base, base)).toBe("Nothing restarts.");
    expect(
      predict(
        base,
        edit(base, (c) => {
          c.frontends.ide.enabled = true;
        }),
      ),
    ).toBe("Starts the IDE.");
    expect(
      predict(
        base,
        edit(base, (c) => {
          c.frontends.terminal.enabled = false;
        }),
      ),
    ).toBe("Stops the terminal.");
    expect(
      predict(
        base,
        edit(base, (c) => {
          c.runtime.tz = "Europe/Rome";
        }),
      ),
    ).toBe("Restarts the terminal; your terminal sessions keep running.");
    const both = edit(base, (c) => {
      c.frontends.ide.enabled = true;
    });
    expect(
      predict(
        both,
        edit(both, (c) => {
          c.runtime.tz = "Europe/Rome";
        }),
      ),
    ).toBe("Restarts the terminal and the IDE; your terminal sessions keep running.");
  });
});

describe("readImport", () => {
  it("rejects files that are not exports", () => {
    expect(readImport("not json", base)).toEqual({ ok: false, error: "That file is not JSON." });
    expect(readImport('{"schema":7}', base)).toEqual({
      ok: false,
      error: "That file is not a Berth config export.",
    });
  });

  it("lists what changes and patches without schema or revision", () => {
    const file = JSON.stringify({
      config: { ...base, revision: 9, runtime: { tz: "Europe/Rome" } },
    });
    const parsed = readImport(file, base);
    expect(parsed).toEqual({
      ok: true,
      patch: { frontends: base.frontends, agents: base.agents, runtime: { tz: "Europe/Rome" } },
      rows: [{ path: "runtime.tz", from: '"Etc/UTC"', to: '"Europe/Rome"' }],
    });
    expect(readImport(JSON.stringify(base), base)).toMatchObject({ ok: true, rows: [] });
  });

  it("accepts schema 2 exports and lists MCP changes", () => {
    const mcp = {
      servers: { linear: { url: "https://l.example/mcp", auth: "none", enabled: true } },
      available: {},
    };
    const base2: Config = { ...base, schema: 2, mcp: { servers: {}, available: {} } };
    const parsed = readImport(JSON.stringify({ ...base2, mcp }), base2);
    expect(parsed).toMatchObject({ ok: true, rows: [{ path: "mcp.servers" }] });
  });

  it("lists skill source changes", () => {
    const x = {
      repo: "https://github.com/anthropics/skills",
      ref: "main",
      path: "skills",
      commit: "0123456789abcdef0123456789abcdef01234567",
      found: { pdf: { path: "skills/pdf", description: "PDF tools" } },
      attached: { pdf: { enabled: true, agents: null, workspaces: {} } },
    };
    const base6: Config = {
      ...base,
      schema: 6,
      skills: { sources: {} },
    };
    const parsed = readImport(JSON.stringify({ ...base6, skills: { sources: { x } } }), base6);
    expect(parsed).toMatchObject({ ok: true, rows: [{ path: "skills.sources" }] });
  });

  it("finds the config in an export", () => {
    expect(exportedConfig('{"config":{"schema":3}}')).toEqual({ ok: true, cfg: { schema: 3 } });
    expect(exportedConfig('{"schema":0}')).toMatchObject({ ok: false });
  });

  it("accepts schema 4 exports and lists added agents", () => {
    const base4: Config = {
      ...base,
      schema: 4,
      agents: { ...base.agents, added: [] },
      mcp: { servers: {}, available: {} },
      skills: {},
    };
    const file = JSON.stringify({
      ...base4,
      agents: { ...base4.agents, added: ["@github/copilot"] },
    });
    expect(readImport(file, base4)).toMatchObject({ ok: true, rows: [{ path: "agents.added" }] });
  });

  it("accepts schema 5 exports and lists workspaces", () => {
    const base5: Config = {
      ...base,
      schema: 5,
      agents: { ...base.agents, added: [] },
      mcp: { servers: {}, available: {} },
      skills: {},
      workspaces: {},
    };
    const file = JSON.stringify({ ...base5, workspaces: { api: { path: "acme/api", repo: "" } } });
    expect(readImport(file, base5)).toMatchObject({ ok: true, rows: [{ path: "workspaces" }] });
    expect(readImport(JSON.stringify({ ...base5, schema: 7 }), base5)).toMatchObject({ ok: false });
  });
});
