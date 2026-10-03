// Settings drafts: dotted-path access, the patch between two configs, and
// rebasing edits onto a newer revision after a conflict.

export function getPath(obj, path) {
  return path.split(".").reduce((o, k) => (o == null ? undefined : o[k]), obj);
}

export function setPath(obj, path, value) {
  const keys = path.split(".");
  let o = obj;
  for (const k of keys.slice(0, -1)) o = o[k] = o[k] || {};
  o[keys[keys.length - 1]] = value;
}

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

/** diff is the patch that turns base into draft, limited to paths. */
export function diff(base, draft, paths) {
  const patch = {};
  const changed = [];
  for (const p of paths) {
    if (!same(getPath(base, p), getPath(draft, p))) {
      setPath(patch, p, getPath(draft, p));
      changed.push(p);
    }
  }
  return { patch, changed };
}

/** rebase replays the edits draft made to old on top of server. */
export function rebase(old, draft, server, paths) {
  const next = structuredClone(server);
  for (const p of diff(old, draft, paths).changed) setPath(next, p, structuredClone(getPath(draft, p)));
  return next;
}
