// Agents: update source and actions, per-package toggles and keys, history.
import { verb, applyConfig, errorText, fieldErrors } from "../api.js";
import { h, clear, badge, button, busy, card, pageHeader, field, input, toggle, secretField, alert, toast,
  confirmDialog, empty, skeleton, when, short, describeSource } from "../ui.js";
import { runJob } from "../jobs.js";

export function render(ctx) {
  const sourceBox = h("div", null, skeleton(2));
  const agentsBox = h("div", { class: "grid" });
  const mcpBox = h("div", { class: "grid" });
  const historyBox = h("div");
  const el = h("div", { class: "page" },
    pageHeader("Agents", "Pinned agent CLIs and MCP servers. Switch each on or off and give them API keys."),
    sourceBox,
    h("h2", { class: "section-title", text: "Agents" }), agentsBox,
    h("h2", { class: "section-title", text: "MCP servers" }), mcpBox,
    h("h2", { class: "section-title", text: "History" }), historyBox);

  let ref = "main";

  async function loadSource() {
    const r = await verb("config.get");
    if (r.ok) ref = r.body.config.agents.ref;
    drawSource();
  }

  function finishJob(job, okText) {
    if (!job) return;
    toast(job.state === "succeeded" ? okText : "The job failed; open Activity for its log.", job.state === "succeeded" ? "ok" : "bad");
    ctx.refresh();
  }

  function drawSource() {
    const refInput = input({ value: ref, spellcheck: "false", autocomplete: "off" });
    const refField = field({ label: "Update source", input: refInput, mono: true, hint: "A branch, tag or commit of the Berth repository." });
    const result = h("div", { "aria-live": "polite" });
    const saveRef = button("Save", { compact: true });
    saveRef.addEventListener("click", async () => {
      const done = busy(saveRef, "Saving…");
      const r = await applyConfig({ agents: { ref: refInput.value.trim() } });
      done();
      refField.setError(fieldErrors(r)["agents.ref"] || (r.ok ? "" : errorText(r)));
      if (r.ok) {
        ref = r.body.config.agents.ref;
        toast("Updates now come from " + ref + ".");
      }
    });
    const check = button("Check for updates");
    check.addEventListener("click", async () => {
      const done = busy(check, "Checking…");
      const r = await verb("update.check", { force: true });
      done();
      const c = r.ok && r.body.check;
      if (!c) clear(result, alert("bad", errorText(r)));
      else if (c.update_available === null) clear(result, alert("warn", "Could not reach " + c.ref + ": " + c.error));
      else if (c.update_available) clear(result, alert("warn", "Update available on " + c.ref + ": " + short(c.hash) + "."));
      else clear(result, alert("ok", "Up to date with " + c.ref + "."));
      ctx.refresh();
    });
    const update = button("Update", { kind: "primary" });
    update.addEventListener("click", () => confirmDialog({
      title: "Update the agents from " + ref + "?",
      body: "The new set installs beside the current one and switches atomically. Running agent sessions keep their version.",
      confirm: "Update",
      action: async () => {
        runJob("update", {}).then((job) => finishJob(job, "Agents updated."));
        toast("Update started; follow it in Activity.", "info");
        return true;
      },
    }));
    const rollback = button("Roll back");
    rollback.addEventListener("click", () => confirmDialog({
      title: "Roll back to the previous agent set?",
      body: "Berth switches back at once, offline. Restarts keep this choice until the next update.",
      confirm: "Roll back", danger: true,
      action: async () => {
        runJob("rollback", {}).then((job) => finishJob(job, "Rolled back."));
        return true;
      },
    }));
    const gc = button("Clean up old versions");
    gc.addEventListener("click", () => confirmDialog({
      title: "Remove unused agent sets?",
      body: "Current, previous, any set in use, and the configured number of kept sets stay.",
      confirm: "Clean up",
      action: async () => {
        runJob("gc", {}).then((job) => finishJob(job, "Old versions removed."));
        return true;
      },
    }));
    clear(sourceBox, card({
      body: [h("div", { class: "row" }, h("div", { class: "spacer" }, refField.el), saveRef), result],
      footer: [check, update, rollback, gc],
    }));
  }

  function packageCard(p) {
    const tg = toggle({
      checked: p.enabled, label: (p.enabled ? "Turn off " : "Turn on ") + p.name,
      onchange: async (on) => {
        tg.button.disabled = true;
        const st = ctx.status();
        const disabled = new Set((st.packages || []).filter((x) => !x.enabled).map((x) => x.package));
        if (on) disabled.delete(p.package);
        else disabled.add(p.package);
        const r = await applyConfig({ agents: { disabled: [...disabled] } });
        tg.button.disabled = false;
        if (!r.ok) {
          tg.set(!on);
          toast(errorText(r), "bad");
          return;
        }
        toast(p.name + (on ? " is on." : " is off.") + " MCP registrations update in the background.");
        ctx.refresh();
      },
    });
    const keys = p.keys.map((k) => secretField({
      name: k.name, set: k.set,
      onSave: async (value) => {
        const r = await applyConfig(null, { [k.name]: value });
        if (!r.ok) return fieldErrors(r)["secrets." + k.name] || errorText(r);
        toast(k.name + " saved.");
        ctx.refresh();
        return null;
      },
      onRemove: async () => {
        const r = await applyConfig(null, { [k.name]: null });
        if (!r.ok) return errorText(r);
        toast(k.name + " removed.");
        ctx.refresh();
        return null;
      },
    }));
    return card({
      title: p.name, badge: badge(p.enabled ? "on" : "off"),
      actions: [tg.el],
      body: [
        h("p", { class: "muted" }, h("span", { class: "mono", text: p.package + " " + p.version })),
        keys.length ? keys : h("p", { class: "muted small", text: "Needs no API key." }),
      ],
    });
  }

  function drawHistory(st) {
    const versions = (st.agents && st.agents.versions) || [];
    const cur = st.agents && st.agents.current && st.agents.current.hash;
    const prev = st.agents && st.agents.previous && st.agents.previous.hash;
    if (!versions.length) {
      clear(historyBox, card({ body: empty("No agent set is installed yet.") }));
      return;
    }
    clear(historyBox, h("div", { class: "card history" }, versions.map((v) => {
      const label = v.hash === cur ? "Current" : v.hash === prev ? "Previous" : "Kept";
      const pkgs = Object.keys(v.packages || {}).sort();
      return h("details", null,
        h("summary", null, h("strong", { text: label }), h("span", { class: "mono", text: short(v.hash) }),
          h("span", { text: describeSource(v.source) }), h("span", { class: "muted", text: when(v.installed_at) })),
        h("div", { class: "packages" }, pkgs.map((n) => h("div", { class: "mono", text: n + " " + v.packages[n] }))));
    })));
  }

  // A redraw replaces the cards and their toggles. That is fine between
  // actions, but it must not interrupt typing, so only redraw when the
  // package list itself changed.
  let lastKey = "";
  const off = ctx.onStatus((st) => {
    const pkgs = st.packages || [];
    const key = JSON.stringify(pkgs);
    if (key !== lastKey) {
      lastKey = key;
      const agentsList = pkgs.filter((p) => p.kind === "agent");
      const mcpList = pkgs.filter((p) => p.kind === "mcp");
      clear(agentsBox, agentsList.length ? agentsList.map(packageCard) : empty("No agents installed yet."));
      clear(mcpBox, mcpList.length ? mcpList.map(packageCard) : empty("No MCP servers installed yet."));
    }
    drawHistory(st);
  });
  loadSource();
  return { el, dispose: off };
}
