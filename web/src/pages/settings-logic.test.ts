import { describe, expect, it } from "vitest";
import type { Config } from "@/api/types";
import { predict, readImport } from "./settings-logic";

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
    expect(readImport('{"schema":3}', base)).toEqual({
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
});
