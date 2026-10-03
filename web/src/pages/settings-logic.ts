import type { Config } from "@/api/types";
import { getPath } from "@/lib/draft";

// The settings this page edits, by dotted path.
export const PATHS = [
  "frontends.terminal.enabled",
  "frontends.ide.enabled",
  "runtime.tz",
  "agents.keep",
  "agents.boot_timeout",
  "agents.mcp_timeout",
] as const;

/** predict says, before saving, which services a change restarts. */
export function predict(base: Config, draft: Config): string {
  const out: string[] = [];
  const was = { t: base.frontends.terminal.enabled, i: base.frontends.ide.enabled };
  const now = { t: draft.frontends.terminal.enabled, i: draft.frontends.ide.enabled };
  if (was.t !== now.t) out.push(now.t ? "Starts the terminal" : "Stops the terminal");
  if (was.i !== now.i) out.push(now.i ? "Starts the IDE" : "Stops the IDE");
  if (base.runtime.tz !== draft.runtime.tz) {
    const r: string[] = [];
    if (was.t && now.t) r.push("terminal");
    if (was.i && now.i) r.push("IDE");
    if (r.length)
      out.push(`Restarts the ${r.join(" and the ")}; your terminal sessions keep running`);
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

/** readImport checks an exported config file against the current config. */
export function readImport(text: string, base: Config): ParsedImport {
  let doc: unknown;
  try {
    doc = JSON.parse(text);
  } catch {
    return { ok: false, error: "That file is not JSON." };
  }
  const cfg = isObject(doc) && isObject(doc.config) ? doc.config : doc;
  if (!isObject(cfg) || ![1, 2, 3].includes(cfg.schema as number))
    return { ok: false, error: "That file is not a Berth config export." };
  const patch = { ...cfg };
  delete patch.schema;
  delete patch.revision;
  const rows = [...PATHS, "agents.ref", "agents.disabled", "mcp.servers", "mcp.available", "skills"]
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
