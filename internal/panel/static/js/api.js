// Talks to the panel API: same-origin JSON only. State-changing calls carry
// the header the server's CSRF check requires. A lost session goes back to
// sign-in, and an unfinished setup goes to the wizard.
const HEADERS = { "Content-Type": "application/json", "X-Berth-Panel": "1" };

let online = true;
const watchers = new Set();

function setOnline(value) {
  if (online === value) return;
  online = value;
  for (const fn of watchers) fn(online);
}

/** onConnection calls fn(online) whenever the panel becomes reachable or not. */
export function onConnection(fn) {
  watchers.add(fn);
  return () => watchers.delete(fn);
}

async function request(path, init) {
  let res;
  try {
    res = await fetch(path, Object.assign({ cache: "no-store" }, init));
  } catch (_) {
    setOnline(false);
    return { ok: false, status: 0, body: { error: { code: "network", message: "The panel is not reachable." } } };
  }
  setOnline(true);
  let body = null;
  try {
    body = await res.json();
  } catch (_) {
    body = null;
  }
  const code = body && body.error && body.error.code;
  if (res.status === 401 && path !== "/api/login" && !location.pathname.startsWith("/login")) {
    location.replace("/login");
  } else if (res.status === 409 && code === "setup_required" && location.pathname !== "/setup") {
    location.replace("/setup");
  }
  return { ok: res.ok, status: res.status, body };
}

export function get(path) {
  return request(path);
}

export function post(path, body) {
  return request(path, { method: "POST", headers: HEADERS, body: JSON.stringify(body || {}) });
}

export function verb(name, body) {
  return post("/api/verbs/" + name, body);
}

/** errorText turns an error response into one sentence for the operator. */
export function errorText(r, fallback) {
  const e = r && r.body && r.body.error;
  if (!e) return fallback || (r && r.status ? "The panel answered " + r.status + "." : "The request failed.");
  if (e.fields && e.fields.length) {
    return e.fields.map((f) => (f.field ? f.field + ": " : "") + f.message).join(" ");
  }
  return e.message || e.code;
}

/** fieldErrors maps a response's field errors to {field: message}. */
export function fieldErrors(r) {
  const out = {};
  const e = r && r.body && r.body.error;
  if (e && e.fields) for (const f of e.fields) out[f.field] = f.message;
  return out;
}

/** applyConfig applies a patch and/or secrets on top of the latest revision. */
export async function applyConfig(patch, secrets) {
  const body = {};
  if (patch) {
    const cur = await verb("config.get");
    if (!cur.ok) return cur;
    body.base_revision = cur.body.config.revision;
    body.patch = patch;
  }
  if (secrets) body.secrets = secrets;
  return verb("config.apply", body);
}
