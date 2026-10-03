// Berth panel. Renders `berth status --json` and the broker's job state.
// Everything from the server is inserted with textContent, never as HTML.
"use strict";

const $ = (id) => document.getElementById(id);
const ARM_MS = 5000;
const POLL_MS = 1000;

let jobRunning = false;
let polling = false;

function el(tag, text, cls) {
  const node = document.createElement(tag);
  if (text !== undefined && text !== null) node.textContent = text;
  if (cls) node.className = cls;
  return node;
}

function when(iso) {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

function short(sha) {
  return sha ? sha.slice(0, 7) : "";
}

function errorText(body, fallback) {
  const e = body && body.error;
  if (!e) return fallback;
  return (e.step ? e.step + ": " : "") + (e.message || e.code);
}

async function api(path, init) {
  let res;
  try {
    res = await fetch(path, Object.assign({ cache: "no-store" }, init));
  } catch (err) {
    return { ok: false, status: 0, body: { error: { code: "network", message: "panel unreachable: " + err.message } } };
  }
  if (res.status === 401) {
    location.replace("/login");
    return { ok: false, status: 401, body: null };
  }
  let body = null;
  try {
    body = await res.json();
  } catch (_) {
    body = null;
  }
  return { ok: res.ok, status: res.status, body };
}

function banner(message) {
  const b = $("banner");
  b.textContent = message || "";
  b.hidden = !message;
}

function describeSource(s) {
  if (!s) return "unknown";
  switch (s.origin) {
    case "image":
      return "image seed";
    case "ref": {
      let text = (s.repo ? s.repo + " " : "") + "@ " + (s.ref || "?");
      if (s.commit) text += " (" + short(s.commit) + ")";
      return text;
    }
    case "dir":
      return "local directory";
    default:
      return s.origin || "unknown";
  }
}

function definitionList(rows) {
  const dl = el("dl");
  for (const [term, value, cls] of rows) {
    dl.append(el("dt", term), el("dd", value, cls));
  }
  return dl;
}

function renderSet(container, set) {
  container.replaceChildren();
  if (!set) {
    container.append(el("p", "None.", "muted"));
    return;
  }
  container.append(definitionList([
    ["Hash", set.hash, "mono"],
    ["Source", describeSource(set.source)],
    ["Installed", when(set.installed_at)],
  ]));
  const names = Object.keys(set.packages || {}).sort();
  if (names.length === 0) return;
  const table = el("table");
  const head = el("tr");
  head.append(el("th", "Package"), el("th", "Version"));
  table.append(head);
  for (const name of names) {
    const row = el("tr");
    row.append(el("td", name), el("td", set.packages[name]));
    table.append(row);
  }
  const wrap = el("div", null, "table-wrap");
  wrap.append(table);
  container.append(wrap);
}

function renderStatus(st) {
  const agents = st.agents || {};
  if (st.mode === "recovery") {
    banner("Recovery mode: " + (st.config_error || "the config is unusable") +
      ". Run berth config revert or berth config reset as root.");
  }
  if (st.agents_error) banner("Could not read the agent store: " + st.agents_error);
  const b = agents.build || {};
  $("build").textContent = b.version
    ? "berth " + b.version + (b.commit ? " (" + short(b.commit) + ")" : "") + (b.arch ? " · " + b.arch : "")
    : "";
  $("mode").textContent = agents.follow_image ? "follows the image" : "pinned";
  $("mode").title = agents.follow_image
    ? "Restarts install the agent set shipped with a newer image."
    : "Restarts keep this agent set.";
  renderSet($("current"), agents.current);
  renderSet($("previous"), agents.previous);
  $("rollback").hidden = !agents.previous;
  $("warnings").replaceChildren(...(agents.warnings || []).map((w) => el("li", w)));
}

async function loadStatus() {
  const r = await api("/api/status");
  if (!r.ok || !r.body) {
    banner("Could not read status: " + errorText(r.body, "HTTP " + r.status));
    return null;
  }
  banner("");
  renderStatus(r.body);
  return r.body;
}

function post(verb, input) {
  return api("/api/verbs/" + verb, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Berth-Panel": "1" },
    body: JSON.stringify(input || {}),
  });
}

async function check() {
  const out = $("check-result");
  const button = $("check");
  button.disabled = true;
  out.hidden = false;
  out.className = "muted";
  out.textContent = "Checking the agent lock on main…";
  const r = await post("update.check");
  button.disabled = false;
  if (!r.ok || !r.body || !r.body.check) {
    out.className = "error";
    out.textContent = "Check failed: " + errorText(r.body, "HTTP " + r.status);
    return;
  }
  const c = r.body.check;
  if (c.update_available) {
    out.className = "";
    out.textContent = "Update available on " + c.ref + ": " + c.hash + " (" + short(c.commit) + ").";
  } else {
    out.className = "ok";
    out.textContent = "Up to date with " + c.ref + ".";
  }
}

function summarize(job) {
  const res = job.result || {};
  if (job.state === "running") return "Running…";
  if (job.state === "failed") {
    const exit = job.exit_code === null || job.exit_code === undefined ? "" : " (exit " + job.exit_code + ")";
    return errorText(res, "failed") + exit;
  }
  const hash = res.current ? res.current.hash : "none";
  if (job.verb === "rollback") return "Now on " + hash + ".";
  if (res.changed === false) return "Already on " + hash + "; nothing to do.";
  if (res.installed) return "Installed and activated " + hash + ".";
  return "Activated " + hash + ".";
}

function renderJob(job) {
  const box = $("job");
  const pill = $("job-state");
  const logBox = $("log-box");
  box.replaceChildren();
  pill.className = "pill";
  pill.textContent = "";
  if (!job) {
    box.append(el("p", "No job has run since the container started.", "muted"));
    logBox.hidden = true;
    setRunning(false);
    return;
  }
  pill.textContent = job.state;
  pill.classList.add(job.state);
  box.append(definitionList([
    ["Command", "berth " + (job.args || []).join(" "), "mono"],
    ["Started", when(job.started_at)],
    ["Finished", job.finished_at ? when(job.finished_at) : "—"],
    ["Result", summarize(job), job.state === "failed" ? "error" : ""],
  ]));

  const log = $("log");
  const atBottom = log.scrollTop + log.clientHeight >= log.scrollHeight - 4;
  log.textContent = (job.log_truncated ? "… earlier output truncated …\n" : "") + (job.log || "");
  logBox.hidden = !job.log;
  if (job.state === "running") logBox.open = true;
  if (atBottom) log.scrollTop = log.scrollHeight;
  setRunning(job.state === "running");
}

function setRunning(running) {
  jobRunning = running;
  $("update").disabled = running;
  $("rollback").disabled = running;
}

async function latestJob() {
  const list = await api("/api/jobs");
  if (!list.ok || !list.body) return list;
  const newest = (list.body.jobs || [])[0];
  if (!newest) return { ok: true, body: { job: null } };
  return api("/api/jobs/" + newest.id);
}

async function poll() {
  if (polling) return;
  polling = true;
  try {
    for (;;) {
      const r = await latestJob();
      if (!r.ok || !r.body) {
        banner("Could not read the job: " + errorText(r.body, "HTTP " + r.status));
        return;
      }
      renderJob(r.body.job);
      if (!r.body.job || r.body.job.state !== "running") {
        await loadStatus();
        return;
      }
      await new Promise((resolve) => setTimeout(resolve, POLL_MS));
    }
  } finally {
    polling = false;
  }
}

async function startJob(request) {
  const verb = request.verb;
  delete request.verb;
  const r = await post(verb, request);
  if (r.status === 409) {
    banner("Another job is already running.");
  } else if (!r.ok) {
    banner("Could not start the job: " + errorText(r.body, "HTTP " + r.status));
    return;
  } else {
    banner("");
    renderJob(r.body.job);
  }
  poll();
}

// armed makes a button ask for a second click within ARM_MS.
function armed(button, prompt, action) {
  const label = button.textContent;
  let timer = null;
  const disarm = () => {
    clearTimeout(timer);
    timer = null;
    button.textContent = label;
  };
  return () => {
    if (jobRunning) return;
    if (timer === null) {
      const text = prompt();
      if (!text) return;
      button.textContent = text;
      timer = setTimeout(disarm, ARM_MS);
      return;
    }
    disarm();
    action();
  };
}

function updateRequest() {
  const source = document.querySelector('input[name="source"]:checked').value;
  if (source === "image") return { verb: "update", image: true };
  if (source === "ref") {
    const ref = $("ref").value.trim();
    return ref ? { verb: "update", ref } : null;
  }
  return { verb: "update", ref: "main" };
}

function init() {
  $("logout").addEventListener("click", async () => {
    await api("/api/logout", { method: "POST", headers: { "Content-Type": "application/json", "X-Berth-Panel": "1" }, body: "{}" });
    location.replace("/login");
  });
  // The request is fixed when the button is armed, so the confirmed job is
  // the one that runs even if the form changes before the second click.
  let pending = null;
  const update = armed($("update"), () => {
    pending = updateRequest();
    if (!pending) {
      banner("Enter a git ref to update from.");
      $("ref").focus();
      return null;
    }
    banner("");
    if (pending.image) return "Confirm: install the image's set";
    return "Confirm: install from " + (pending.ref || "main");
  }, () => startJob(pending));
  $("update-form").addEventListener("submit", (e) => {
    e.preventDefault();
    update();
  });
  $("ref").addEventListener("focus", () => {
    document.querySelector('input[name="source"][value="ref"]').checked = true;
  });
  $("rollback").addEventListener("click", armed($("rollback"), () => "Confirm rollback", () => startJob({ verb: "rollback" })));
  $("check").addEventListener("click", check);
  loadStatus();
  poll();
}

document.addEventListener("DOMContentLoaded", init);
