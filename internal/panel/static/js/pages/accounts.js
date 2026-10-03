// Accounts: git identity and the GitHub and GitLab CLIs (device sign-in).
import { get, verb, errorText } from "../api.js";
import { h, clear, badge, button, busy, card, pageHeader, field, input, alert, toast, skeleton } from "../ui.js";
import { runJob, watchJob } from "../jobs.js";
import { icon } from "../icons.js";

const FORGES = [["gh", "GitHub CLI"], ["glab", "GitLab CLI"]];

export function render() {
  const gitBox = h("div", null, skeleton(2));
  const forgeBox = h("div", { class: "grid" });
  const el = h("div", { class: "page narrow" },
    pageHeader("Accounts", "Your git identity and code-host sign-ins. Tokens stay in your home volume."),
    gitBox, forgeBox);
  const stops = [];

  async function load() {
    const r = await verb("forge.status");
    if (!r.ok) {
      clear(gitBox, alert("bad", errorText(r)));
      return;
    }
    drawGit(r.body.git);
    const jobs = await get("/api/jobs");
    const running = ((jobs.ok && jobs.body.jobs) || []).find((j) => j.verb === "forge.login" && j.state === "running");
    clear(forgeBox, FORGES.map(([id, label]) => forgeCard(id, label, r.body.forges[id], running)));
  }

  function drawGit(git) {
    const name = input({ value: git.name || "", autocomplete: "name" });
    const email = input({ value: git.email || "", type: "email", autocomplete: "email" });
    const status = h("div", { "aria-live": "polite" });
    const save = button("Save", { kind: "primary", type: "submit" });
    const form = h("form", { class: "form", novalidate: true },
      h("div", { class: "form-row" }, field({ label: "Name", input: name }).el, field({ label: "Email", input: email }).el),
      status, h("div", { class: "row" }, save));
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const done = busy(save, "Saving…");
      const r = await verb("git.identity", { name: name.value.trim(), email: email.value.trim() });
      done();
      clear(status, r.ok ? [] : alert("bad", errorText(r)));
      if (r.ok) toast("Git identity saved.");
    });
    clear(gitBox, card({ title: "Git identity", body: form }));
  }

  function deviceBox(device) {
    const copy = button("Copy code", { compact: true, icon: "copy", onclick: async () => {
      try {
        await navigator.clipboard.writeText(device.code);
        toast("Code copied.");
      } catch (_) {
        toast("Copy failed; type the code instead.", "bad");
      }
    } });
    return h("div", { class: "device" },
      h("span", { class: "muted", text: "Enter this code on the code host:" }),
      h("span", { class: "device-code", text: device.code }),
      h("div", { class: "row" }, copy,
        h("a", { class: "btn secondary compact", href: device.url, target: "_blank", rel: "noopener noreferrer" },
          icon("external", 13, 1.6), "Open " + device.url.replace(/^https:\/\//, "").replace(/\?.*$/, ""))),
      h("div", { class: "row" }, h("span", { class: "spinner", "aria-hidden": "true" }), badge("waiting", "for approval")));
  }

  function forgeCard(id, label, f, running) {
    const body = h("div", { class: "stack" });
    const signedIn = f && f.signed_in;
    const facts = h("p", { class: "muted", text: signedIn ? (f.user || "Signed in") + " on " + f.host : "Not signed in to " + (f ? f.host : "") + "." });
    const signIn = button(signedIn ? "Sign in again" : "Sign in", { compact: true, kind: signedIn ? "secondary" : "primary" });
    const follow = (job) => {
      clear(body, facts, job.device ? deviceBox(job.device) : h("div", { class: "row" },
        h("span", { class: "spinner", "aria-hidden": "true" }), h("span", { text: "Starting the sign-in…" })));
      if (job.state !== "running") {
        toast(job.state === "succeeded" ? label + ": signed in." : label + ": sign-in did not complete; see Activity.", job.state === "succeeded" ? "ok" : "bad");
        load();
      }
    };
    signIn.addEventListener("click", async () => {
      signIn.disabled = true;
      await runJob("forge.login", { forge: id }, follow);
    });
    clear(body, facts);
    if (running && (running.args || []).some((a) => a.endsWith(id))) {
      signIn.disabled = true;
      stops.push(watchJob(running.id, follow));
    }
    return card({ title: label, badge: badge(signedIn ? "signed_in" : "signed_out"), body, footer: signIn });
  }

  load();
  return { el, dispose() { for (const stop of stops) stop(); } };
}
