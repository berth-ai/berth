// Overview: service health, the current agent set, update callout, recent jobs.
import { get, verb, errorText } from "../api.js";
import { h, clear, badge, button, busy, card, pageHeader, facts, table, empty, skeleton, alert, toast,
  since, when, short, describeSource, SERVICE_LABELS } from "../ui.js";
import { runJob, jobTitle } from "../jobs.js";

const SERVICES = ["panel", "ttyd", "code_server"];

export function render(ctx) {
  const callout = h("div");
  const services = h("div", { class: "grid" }, skeleton(3));
  const agentsBox = h("div");
  const jobsBox = h("div");
  const el = h("div", { class: "page" },
    pageHeader("Overview", "Health of your Berth and quick access to its frontends.",
      h("a", { class: "btn primary", href: "#terminal" }, "Open Terminal"),
      h("a", { class: "btn secondary", href: "#ide" }, "Open IDE")),
    callout,
    h("h2", { class: "section-title", text: "Services" }), services,
    h("div", { class: "grid" }, agentsBox, jobsBox));

  function serviceCard(name, svc) {
    const label = SERVICE_LABELS[name];
    if (!svc) {
      return card({ title: label, badge: badge("disabled"), body: h("p", { class: "muted", text: "Switched off." }),
        footer: h("a", { class: "btn secondary compact", href: "#settings" }, "Settings") });
    }
    const failed = svc.state === "failed";
    const body = [facts([
      ["Uptime", svc.started_at ? since(svc.started_at) : "—"],
      ["Restarts", String(svc.restarts)],
    ])];
    let footer = null;
    if (failed || svc.state === "restarting") {
      const lines = (svc.log || []).slice(-8).join("\n");
      body.push(h("pre", { class: "log mini", text: lines || "No output." }));
      const retry = button("Retry", { compact: true, kind: "primary" });
      retry.addEventListener("click", async () => {
        const done = busy(retry, "Restarting…");
        const r = await verb("service.restart", { name });
        done();
        if (!r.ok) toast(errorText(r), "bad");
        ctx.refresh();
      });
      footer = retry;
    }
    return card({ title: label, badge: badge(svc.state, svc.restarts ? svc.restarts + " restarts" : null),
      body, footer, cls: failed ? "failed" : null });
  }

  function draw(st) {
    clear(services, SERVICES.map((n) => serviceCard(n, (st.services || []).find((s) => s.name === n))));
    const cur = st.agents && st.agents.current;
    clear(agentsBox, card({
      title: "Agent set",
      badge: st.agents && st.agents.follow_image ? badge("on", "follows the image") : null,
      body: cur ? facts([
        ["Lock", h("span", { class: "mono", text: short(cur.hash) })],
        ["Origin", describeSource(cur.source)],
        ["Installed", when(cur.installed_at)],
        ["Packages", String(Object.keys(cur.packages || {}).length)],
      ]) : alert("warn", st.agents_error || "No agent set is installed yet. The first start installs one; watch Activity."),
      footer: h("a", { class: "btn secondary compact", href: "#agents" }, "Manage agents"),
    }));
    const check = st.check;
    if (check && check.update_available) {
      const update = button("Update", { compact: true, kind: "primary" });
      update.addEventListener("click", async () => {
        const done = busy(update, "Updating…");
        const job = await runJob("update", {});
        done();
        if (job) toast(job.state === "succeeded" ? "Agents updated." : "The update failed; see Activity.", job.state === "succeeded" ? "ok" : "bad");
        ctx.refresh();
      });
      clear(callout, alert("warn", "Agent update available on " + check.ref + ": " + short(check.hash) + ".", update));
    } else {
      clear(callout);
    }
  }

  async function loadJobs() {
    const r = await get("/api/jobs");
    const jobs = (r.ok && r.body && r.body.jobs) || [];
    clear(jobsBox, card({
      title: "Recent jobs",
      body: jobs.length ? table([{ label: "Job" }, { label: "Status" }, { label: "Started" }],
        jobs.slice(0, 5).map((j) => [
          h("a", { class: "cell-primary", href: "#activity/" + j.id, text: jobTitle(j) }),
          badge(j.state === "running" ? "in_progress" : j.state, null, true),
          when(j.started_at),
        ])) : empty("No jobs yet."),
      footer: h("a", { class: "btn secondary compact", href: "#activity" }, "All activity"),
    }));
  }

  const off = ctx.onStatus(draw);
  loadJobs();
  const timer = setInterval(loadJobs, 5000);
  return { el, dispose() { off(); clearInterval(timer); } };
}
