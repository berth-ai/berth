import type { Workspace } from "@/api/types";

export interface EnvRow {
  key: string;
  secret: boolean;
  /** value is empty for a secret. */
  value: string;
  /** set: a secret has a value; always true for a plain variable. */
  set: boolean;
}

export const secretName = (id: string, key: string) => `workspaces.${id}.${key}`;

/** envRows are workspace id's variables, by name. */
export function envRows(
  id: string,
  ws: Workspace,
  secrets: Record<string, { set: boolean }>,
): EnvRow[] {
  return Object.entries(ws.env ?? {})
    .map(([key, e]) => ({
      key,
      secret: e.secret,
      value: e.secret ? "" : e.value,
      set: e.secret ? Boolean(secrets[secretName(id, key)]?.set) : true,
    }))
    .sort((a, b) => a.key.localeCompare(b.key));
}

const RESERVED = ["PATH", "HOME", "USER", "SHELL", "TERM"];
const RESERVED_PREFIXES = ["TMUX", "LD_", "BERTH_", "GIT_", "SSH_"];

/** envKeyError mirrors config's key rules; existing are the names in use. */
export function envKeyError(key: string, existing: string[]): string {
  if (!/^[A-Z_][A-Z0-9_]{0,127}$/.test(key))
    return "Use capital letters, digits and underscores, starting with a letter or underscore.";
  if (RESERVED.includes(key) || RESERVED_PREFIXES.some((p) => key.startsWith(p)))
    return `${key} is reserved for Berth and the terminal.`;
  if (existing.includes(key)) return `${key} exists; edit it instead.`;
  return "";
}

/** envValueError mirrors config.CheckEnvValue. */
export function envValueError(value: string): string {
  if (value.includes("\u0000")) return "The value cannot contain a NUL character.";
  if (new TextEncoder().encode(value).length > 4096) return "The value must be at most 4 KiB.";
  return "";
}

const envPatch = (id: string, key: string, v: unknown) => ({
  workspaces: { [id]: { env: { [key]: v } } },
});
export const setPlainPatch = (id: string, key: string, value: string) =>
  envPatch(id, key, { value, secret: false });
export const setSecretPatch = (id: string, key: string) =>
  envPatch(id, key, { value: "", secret: true });
export const removePatch = (id: string, key: string) => envPatch(id, key, null);
