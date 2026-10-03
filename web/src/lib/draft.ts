// Settings drafts: dotted-path access, the patch between two configs, and
// rebasing edits onto a newer revision after a conflict.
type Tree = Record<string, unknown>;

const isTree = (v: unknown): v is Tree => typeof v === "object" && v !== null;

export function getPath(obj: unknown, path: string): unknown {
  return path.split(".").reduce<unknown>((o, k) => (isTree(o) ? o[k] : undefined), obj);
}

export function setPath(obj: object, path: string, value: unknown): void {
  const keys = path.split(".");
  let o = obj as Tree;
  for (const k of keys.slice(0, -1)) {
    const next = o[k];
    if (!isTree(next)) o[k] = {};
    o = o[k] as Tree;
  }
  o[keys[keys.length - 1]] = value;
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/** diff is the patch that turns base into draft, limited to paths. */
export function diff(
  base: unknown,
  draft: unknown,
  paths: readonly string[],
): { patch: Tree; changed: string[] } {
  const patch: Tree = {};
  const changed: string[] = [];
  for (const p of paths) {
    if (!same(getPath(base, p), getPath(draft, p))) {
      setPath(patch, p, getPath(draft, p));
      changed.push(p);
    }
  }
  return { patch, changed };
}

/** rebase replays the edits draft made to old on top of server. */
export function rebase<T extends object>(old: T, draft: T, server: T, paths: readonly string[]): T {
  const next = structuredClone(server);
  for (const p of diff(old, draft, paths).changed)
    setPath(next, p, structuredClone(getPath(draft, p)));
  return next;
}
