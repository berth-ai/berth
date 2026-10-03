// Activity: job history and one job's live log.
import { get } from "../api.js";
import { h, clear, badge, card, pageHeader, table, empty, skeleton, logViewer, facts, alert,
  when, duration } from "../ui.js";
import { watchJob, jobTitle, jobCommand } from "../jobs.js";

function stateBadge(job, small) {
  return badge(job.state === "running" ? "in_progress" : job.state, null, small);
}

function listView() {
  const box = h("div", null, skeleton(5));
  const el = h("div", { class: "page" }, pageHeader("Activity", "Jobs started from the panel since the container started."), box);
  async function load() {
    const r = await get("/api/jobs");
    const jobs = (r.ok && r.body && r.body.jobs) || [];
    clear(box, card({ body: jobs.length ? table(
      [{ label: "Job" }, { label: "Status" }, { label: "Started" }, { label: "Duration", num: true }],
      jobs.map((j) => [
        h("a", { class: "cell-primary", href: "#activity/" + j.id, text: jobTitle(j) }),
        stateBadge(j, true), when(j.started_at), duration(j.started_at, j.finished_at),
      ])) : empty("No jobs yet. Updates, tool installs and sign-ins appear here.") }));
  }
  load();
  const timer = setInterval(load, 3000);
  return { el, dispose: () => clearInterval(timer) };
}

function detailView(id) {
  const head = h("div");
  const info = h("div");
  const device = h("div");
  const log = logViewer();
  const el = h("div", { class: "page" }, head, info, device, log.el);
  const stop = watchJob(id, (job) => {
    clear(head, pageHeader(jobTitle(job), null, h("a", { class: "btn secondary", href: "#activity" }, "All jobs")));
    const result = job.result && job.result.error;
    clear(info, card({ title: "Details", badge: stateBadge(job), body: [
      facts([
        ["Command", h("span", { class: "mono", text: jobCommand(job) })],
        ["Started", when(job.started_at)],
        ["Finished", job.finished_at ? when(job.finished_at) : "—"],
        ["Duration", duration(job.started_at, job.finished_at)],
        ["Exit code", job.exit_code == null ? "—" : String(job.exit_code)],
      ]),
      result ? alert("bad", (result.step ? result.step + ": " : "") + (result.message || result.code)) : null,
    ] }));
    clear(device, job.device && job.state === "running" ? alert("info", "Enter " + job.device.code + " at " + job.device.url + ".") : null);
    log.set(job.log, job.log_truncated);
  });
  get("/api/jobs/" + id).then((r) => {
    if (r.status === 404) clear(el, h("div", { class: "page" }, alert("warn", "That job is no longer in the history."),
      h("a", { class: "btn secondary", href: "#activity" }, "All jobs")));
  });
  return { el, dispose: stop };
}

export function render(ctx, param) {
  return /^\d+$/.test(param || "") ? detailView(Number(param)) : listView();
}
