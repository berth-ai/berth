// Tools: the base track (read-only), the user track, and presets.
import { verb, errorText } from "../api.js";
import { h, clear, badge, button, busy, card, pageHeader, field, input, select, alert, toast, dialog,
  confirmDialog, empty, skeleton, table } from "../ui.js";
import { runJob } from "../jobs.js";

export function render() {
  const baseBox = h("div", null, skeleton(4));
  const userBox = h("div");
  const presetsBox = h("div", { class: "grid" });
  const warnBox = h("div");
  const el = h("div", { class: "page" },
    pageHeader("Tools", "The pinned toolchain, your own tools, and curated presets.",
      button("Add tool", { kind: "primary", icon: "plus", onclick: () => addDialog() })),
    warnBox,
    h("h2", { class: "section-title", text: "Your tools" }), userBox,
    h("h2", { class: "section-title", text: "Presets" }), presetsBox,
    h("h2", { class: "section-title", text: "Built in" }), baseBox);

  async function load() {
    const r = await verb("tools.list");
    if (!r.ok) {
      clear(warnBox, alert("bad", errorText(r)));
      return;
    }
    const { base, user, presets, warnings } = r.body;
    clear(warnBox, (warnings || []).map((w) => alert("warn", w)));
    drawUser(user);
    drawPresets(presets);
    drawBase(base);
  }

  async function finish(job, okText) {
    if (!job) return;
    toast(job.state === "succeeded" ? okText : "The job failed; open Activity for its log.", job.state === "succeeded" ? "ok" : "bad");
    load();
  }

  function drawUser(user) {
    clear(userBox, card({
      body: user.length ? table([{ label: "Tool" }, { label: "Version" }, { label: "" }],
        user.map((t) => [
          h("strong", { text: t.name }),
          h("span", { class: "mono", text: t.version }),
          h("div", { class: "row" },
            button("Upgrade", { compact: true, onclick: () => addDialog(t.name, t.version) }),
            button("Remove", { compact: true, onclick: () => confirmDialog({
              title: "Remove " + t.name + "?",
              body: "Berth runs mise unuse -g " + t.name + " as the agent user. Projects that pin it keep their own version.",
              confirm: "Remove tool", danger: true,
              action: async () => {
                runJob("tools.remove", { tool: t.name }).then((job) => finish(job, t.name + " removed."));
                return true;
              },
            }) })),
        ])) : empty("No tools added yet.", button("Add tool", { onclick: () => addDialog() })),
    }));
  }

  function drawPresets(presets) {
    clear(presetsBox, presets.map((p) => {
      const state = p.state || "not_installed";
      const go = button(state === "outdated" ? "Update" : "Install", { compact: true, kind: "primary" });
      go.addEventListener("click", async () => {
        const done = busy(go, "Installing…");
        const job = await runJob("preset.install", { preset: p.id });
        done();
        finish(job, p.name + " installed.");
      });
      return card({
        title: p.name, badge: badge(state),
        body: [h("p", { class: "muted", text: p.description }),
          Object.entries(p.tools).map(([t, v]) => h("div", { class: "mono", text: t + " " + v }))],
        footer: state === "installed" ? null : go,
      });
    }));
  }

  function drawBase(base) {
    const search = input({ type: "search", placeholder: "Filter", "aria-label": "Filter built-in tools" });
    const body = h("div");
    const draw = () => {
      const q = search.value.trim().toLowerCase();
      const rows = base.filter((t) => t.name.toLowerCase().includes(q));
      clear(body, rows.length ? table([{ label: "Tool" }, { label: "Version" }],
        rows.map((t) => [t.name, h("span", { class: "mono", text: t.version })])) : empty("No tool matches."));
    };
    search.addEventListener("input", draw);
    draw();
    clear(baseBox, card({ title: "Built in", badge: h("span", { class: "muted small", text: base.length + " tools, updated with the image" }),
      actions: [search], body }));
  }

  let registry = null;
  async function addDialog(tool, current) {
    const name = input({ value: tool || "", list: "registry", spellcheck: "false", autocomplete: "off", disabled: !!tool });
    const list = h("datalist", { id: "registry" });
    const fName = field({ label: "Tool", input: name, mono: true, hint: "A name from the mise registry, or backend:owner/repo." });
    const versions = select([], "");
    versions.disabled = true;
    const fVersion = field({ label: "Version", input: versions, mono: true });
    const status = h("div", { "aria-live": "polite" });
    const loadVersions = async () => {
      const t = name.value.trim();
      if (!t) return;
      clear(versions, h("option", { text: "Loading…" }));
      versions.disabled = true;
      const r = await verb("tools.versions", { tool: t });
      if (!r.ok) {
        clear(versions);
        fName.setError(errorText(r));
        return;
      }
      fName.setError("");
      const vs = r.body.versions.slice().reverse();
      clear(versions, vs.map((v) => h("option", { value: v, text: v + (v === current ? " (installed)" : "") })));
      versions.disabled = !vs.length;
    };
    name.addEventListener("change", loadVersions);
    dialog({
      title: tool ? "Upgrade " + tool : "Add a tool",
      body: [fName.el, list, fVersion.el, status],
      actions: [
        { label: "Cancel", onclick: (close) => close() },
        { label: tool ? "Install version" : "Add tool", kind: "primary", onclick: async (close, b) => {
          const t = name.value.trim();
          if (!t || versions.disabled || !versions.value) {
            fVersion.setError("Pick a tool and a version.");
            return;
          }
          const done = busy(b, "Starting…");
          runJob("tools.add", { tool: t, version: versions.value }).then((job) => finish(job, t + " " + versions.value + " installed."));
          done();
          close();
        } },
      ],
    });
    if (tool) loadVersions();
    if (!registry) {
      const r = await verb("tools.registry");
      registry = r.ok ? r.body.tools : [];
    }
    clear(list, registry.map((n) => h("option", { value: n })));
  }

  load();
  return { el };
}
