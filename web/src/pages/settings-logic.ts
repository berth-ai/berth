import type { Config } from "@/api/types";
import { getPath } from "@/lib/draft";

// The settings this page edits, by dotted path.
export const PATHS = [
  "frontends.terminal.enabled",
  "frontends.ide.enabled",
  "frontends.orca.enabled",
  "frontends.orca.pairing_address",
  "runtime.tz",
  "agents.keep",
  "agents.boot_timeout",
  "agents.mcp_timeout",
] as const;

/** predict says, before saving, which services a change restarts. */
export function predict(base: Config, draft: Config): string {
  const out: string[] = [];
  const frontends = (c: Config) => ({
    t: c.frontends.terminal.enabled,
    i: c.frontends.ide.enabled,
    o: !!c.frontends.orca?.enabled,
  });
  const was = frontends(base);
  const now = frontends(draft);
  if (was.t !== now.t) out.push(now.t ? "Starts the terminal" : "Stops the terminal");
  if (was.i !== now.i) out.push(now.i ? "Starts the IDE" : "Stops the IDE");
  if (was.o !== now.o) out.push(now.o ? "Starts Orca" : "Stops Orca");
  if (base.runtime.tz !== draft.runtime.tz) {
    const r: string[] = [];
    if (was.t && now.t) r.push("the terminal");
    if (was.i && now.i) r.push("the IDE");
    if (was.o && now.o) r.push("Orca");
    if (r.length) out.push(`Restarts ${r.join(" and ")}; your terminal sessions keep running`);
  }
  return out.length ? `${out.join(". ")}.` : "Nothing restarts.";
}

export interface ImportRow {
  path: string;
  from: string;
  to: string;
}

export type ParsedImport =
  | { ok: true; patch: Record<string, unknown>; rows: ImportRow[] }
  | { ok: false; error: string };

const isObject = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

export type Exported = { ok: true; cfg: Record<string, unknown> } | { ok: false; error: string };

/** exportedConfig finds the config in an exported file: the file itself,
 * or its config key. */
export function exportedConfig(text: string): Exported {
  let doc: unknown;
  try {
    doc = JSON.parse(text);
  } catch {
    return { ok: false, error: "That file is not JSON." };
  }
  const cfg = isObject(doc) && isObject(doc.config) ? doc.config : doc;
  if (!isObject(cfg) || ![1, 2, 3, 4, 5, 6].includes(cfg.schema as number))
    return { ok: false, error: "That file is not a Berth config export." };
  return { ok: true, cfg };
}

/** readImport checks an exported config, already upgraded by config.upgrade, against the current config. */
export function readImport(text: string, base: Config): ParsedImport {
  const found = exportedConfig(text);
  if (!found.ok) return found;
  const cfg = found.cfg;
  const patch = { ...cfg };
  delete patch.schema;
  delete patch.revision;
  const rows = [
    ...PATHS,
    "agents.ref",
    "agents.disabled",
    "agents.added",
    "mcp.servers",
    "mcp.available",
    "mcp.workspaces",
    "skills.sources",
    "workspaces",
  ]
    .filter(
      (p) =>
        getPath(cfg, p) !== undefined &&
        JSON.stringify(getPath(cfg, p)) !== JSON.stringify(getPath(base, p)),
    )
    .map((p) => ({
      path: p,
      from: JSON.stringify(getPath(base, p)),
      to: JSON.stringify(getPath(cfg, p)),
    }));
  return { ok: true, patch, rows };
}
