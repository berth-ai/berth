// Running-job tracking for the indicator (§4) and per-job polling.
import { get, verb, errorText } from "./api.js";
import { toast } from "./ui.js";

const listeners = new Set();
let running = [];
let timer = null;

async function refresh() {
  const r = await get("/api/jobs");
  if (r.ok && r.body) {
    running = (r.body.jobs || []).filter((j) => j.state === "running");
    for (const fn of listeners) fn(running);
  }
  schedule(running.length ? 1000 : 5000);
}

function schedule(ms) {
  clearTimeout(timer);
  timer = setTimeout(refresh, ms);
}

export function startJobs() {
  refresh();
}

/** pokeJobs refreshes the running list now, after a job was started. */
export function pokeJobs() {
  schedule(0);
}

/** onRunning calls fn(runningJobs) on every change; returns an unsubscribe. */
export function onRunning(fn) {
  listeners.add(fn);
  fn(running);
  return () => listeners.delete(fn);
}

/** watchJob polls one job every second until it finishes. Returns stop. */
export function watchJob(id, onUpdate) {
  let stopped = false;
  (async () => {
    while (!stopped) {
      const r = await get("/api/jobs/" + id);
      if (stopped) return;
      if (r.ok && r.body && r.body.job) {
        onUpdate(r.body.job);
        if (r.body.job.state !== "running") return;
      }
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  })();
  return () => {
    stopped = true;
  };
}

/** runJob starts a job verb and resolves with the finished job, or null if it did not start. */
export async function runJob(name, body, onUpdate) {
  const r = await verb(name, body);
  if (!r.ok || !r.body || !r.body.job) {
    toast(r.status === 409 ? "Another job of this kind is running; wait for it to finish." : errorText(r, "The job did not start."), "bad");
    return null;
  }
  pokeJobs();
  return new Promise((resolve) => {
    const stop = watchJob(r.body.job.id, (job) => {
      if (onUpdate) onUpdate(job);
      if (job.state !== "running") {
        stop();
        pokeJobs();
        resolve(job);
      }
    });
  });
}

const TITLES = {
  update: "Agent update",
  rollback: "Agent rollback",
  gc: "Clean up agent versions",
  "mcp.setup": "MCP setup",
  "tools.add": "Add tool",
  "tools.remove": "Remove tool",
  "preset.install": "Install preset",
};

export function jobTitle(job) {
  if (job.verb === "forge.login") {
    return (job.args || []).some((a) => /glab$/.test(a)) ? "Sign in to GitLab" : "Sign in to GitHub";
  }
  return TITLES[job.verb] || job.verb;
}

/** jobCommand is the command a job runs, as the operator would type it. */
export function jobCommand(job) {
  const args = job.args || [];
  return job.group === "agents" ? "berth " + args.join(" ") : args.map((a) => a.replace(/^.*\//, "")).join(" ");
}
