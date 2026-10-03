// Settings: login, frontends, runtime, advanced, host values, backup.
import { verb, errorText, fieldErrors } from "../api.js";
import { h, clear, button, busy, card, pageHeader, field, input, toggle, alert, toast, saveBar, facts,
  confirmDialog, dialog, skeleton, describeChanges } from "../ui.js";

// The settings this page edits, by dotted path.
const PATHS = ["frontends.terminal.enabled", "frontends.ide.enabled", "runtime.tz",
  "agents.keep", "agents.boot_timeout", "agents.mcp_timeout"];

function getPath(obj, path) {
  return path.split(".").reduce((o, k) => (o == null ? undefined : o[k]), obj);
}

function setPath(obj, path, value) {
  const keys = path.split(".");
  let o = obj;
  for (const k of keys.slice(0, -1)) o = o[k] = o[k] || {};
  o[keys[keys.length - 1]] = value;
}

function diff(base, draft, paths) {
  const patch = {};
  const changed = [];
  for (const p of paths) {
    if (JSON.stringify(getPath(base, p)) !== JSON.stringify(getPath(draft, p))) {
      setPath(patch, p, getPath(draft, p));
      changed.push(p);
    }
  }
  return { patch, changed };
}

/** predict says, before saving, which services a change restarts. */
function predict(base, draft) {
  const out = [];
  const was = { t: base.frontends.terminal.enabled, i: base.frontends.ide.enabled };
  const now = { t: draft.frontends.terminal.enabled, i: draft.frontends.ide.enabled };
  if (was.t !== now.t) out.push(now.t ? "Starts the terminal" : "Stops the terminal");
  if (was.i !== now.i) out.push(now.i ? "Starts the IDE" : "Stops the IDE");
  if (base.runtime.tz !== draft.runtime.tz) {
    const r = [];
    if (was.t && now.t) r.push("terminal");
    if (was.i && now.i) r.push("IDE");
    if (r.length) out.push("Restarts the " + r.join(" and the ") + "; your terminal sessions keep running");
  }
  return out.length ? out.join(". ") + "." : "Nothing restarts.";
}

export function render(ctx) {
  const formBox = h("div", { class: "stack" }, skeleton(6));
  const el = h("div", { class: "page narrow" }, pageHeader("Settings", "Changes apply without restarting the container."), formBox);
  let base = null;
  let draft = null;
  const fields = {};
  const controls = {};
  const conflict = h("div", { "aria-live": "polite" });

  const bar = saveBar({
    onDiscard() {
      draft = structuredClone(base);
      drawForm();
    },
    async onSave(b) {
      const { patch, changed } = diff(base, draft, PATHS);
      if (!changed.length) return;
      const done = busy(b, "Saving…");
      const r = await verb("config.apply", { base_revision: base.revision, patch });
      done();
      for (const f of Object.values(fields)) f.setError("");
      if (r.status === 409 && r.body && r.body.config) {
        base = r.body.config;
        clear(conflict, alert("warn", "Settings changed elsewhere. Your edits are kept on top of the new values; review and save again."));
        refresh();
        return;
      }
      if (!r.ok) {
        const errs = fieldErrors(r);
        for (const [path, msg] of Object.entries(errs)) if (fields[path]) fields[path].setError(msg);
        if (!Object.keys(errs).some((p) => fields[p])) toast(errorText(r), "bad");
        return;
      }
      base = r.body.config;
      draft = structuredClone(base);
      clear(conflict);
      toast(describeChanges(r.body.changes));
      refresh();
      ctx.refresh();
    },
  });

  function refresh() {
    const { changed } = diff(base, draft, PATHS);
    if (changed.length) bar.show(predict(base, draft));
    else bar.hide();
  }

  function numberField(path, label, hint, min, max) {
    const inp = input({ type: "number", min, max, step: 1, value: getPath(draft, path), inputmode: "numeric" });
    inp.addEventListener("input", () => {
      setPath(draft, path, inp.value === "" ? null : Number(inp.value));
      refresh();
    });
    fields[path] = field({ label, input: inp, hint });
    return fields[path].el;
  }

  function frontendRow(path, title, text) {
    const tg = toggle({ checked: getPath(draft, path), label: title, onchange: (v) => {
      setPath(draft, path, v);
      refresh();
    } });
    controls[path] = tg;
    return h("div", { class: "setting" }, h("div", null, h("strong", { text: title }), h("span", { class: "muted", text })), tg.el);
  }

  function drawForm() {
    const tz = input({ value: draft.runtime.tz, list: "zones", spellcheck: "false", autocomplete: "off" });
    tz.addEventListener("input", () => {
      draft.runtime.tz = tz.value.trim();
      refresh();
    });
    fields["runtime.tz"] = field({ label: "Time zone", input: tz, mono: true, hint: "New terminal and IDE sessions use it." });
    const zones = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];
    const st = ctx.status() || {};
    const host = st.host || {};
    const build = (st.agents && st.agents.build) || {};
    clear(formBox,
      loginCard(),
      card({ title: "Frontends", body: [
        frontendRow("frontends.terminal.enabled", "Terminal", "Browser terminal with tmux."),
        frontendRow("frontends.ide.enabled", "IDE", "VS Code in the browser."),
      ] }),
      card({ title: "Runtime", body: [fields["runtime.tz"].el, h("datalist", { id: "zones" }, zones.map((z) => h("option", { value: z })))] }),
      card({ body: h("details", { class: "advanced" }, h("summary", { text: "Advanced" }), h("div", { class: "form-row" },
        numberField("agents.keep", "Versions to keep", "Old agent sets kept besides current and previous.", 0, 50),
        numberField("agents.boot_timeout", "Agent boot timeout (s)", "Time allowed for installing agents at start.", 30, 3600),
        numberField("agents.mcp_timeout", "MCP setup timeout (s)", "Time allowed for registering MCP servers.", 10, 1800))) }),
      card({ title: "Host settings", body: [
        alert("info", "Set in .env on the host, then apply with docker compose up -d."),
        facts([
          ["Bind address", h("span", { class: "mono", text: host.bind_address || "—" })],
          ["Panel port", h("span", { class: "mono", text: host.panel_port || "—" })],
          ["User and group IDs", h("span", { class: "mono", text: (host.puid || "?") + ":" + (host.pgid || "?") })],
          ["Workspaces path", h("span", { class: "mono", text: host.workspaces_dir || "—" })],
          ["Image version", h("span", { class: "mono", text: build.version || "—" })],
        ]),
      ] }),
      backupCard(),
      conflict, bar.el);
    refresh();
  }

  function loginCard() {
    const session = ctx.session();
    const cur = input({ type: "password", autocomplete: "current-password" });
    const user = input({ value: session.user || "", autocomplete: "username" });
    const pw = input({ type: "password", autocomplete: "new-password" });
    const pw2 = input({ type: "password", autocomplete: "new-password" });
    const fc = field({ label: "Current password", input: cur });
    const fu = field({ label: "User", input: user });
    const fp = field({ label: "New password", input: pw, hint: "At least 10 characters." });
    const fr = field({ label: "Repeat new password", input: pw2 });
    const save = button("Change login", { type: "submit" });
    const form = h("form", { class: "form", novalidate: true }, fc.el, h("div", { class: "form-row" }, fu.el), h("div", { class: "form-row" }, fp.el, fr.el), h("div", { class: "row" }, save));
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      fr.setError(pw.value === pw2.value ? "" : "The passwords differ.");
      if (pw.value !== pw2.value) return;
      const done = busy(save, "Changing…");
      const r = await verb("auth.change", { current_password: cur.value, user: user.value.trim(), password: pw.value });
      done();
      const errs = fieldErrors(r);
      fc.setError(r.body && r.body.error && r.body.error.code === "wrong_password" ? "The current password is wrong." : "");
      fu.setError(errs.user || "");
      fp.setError(errs.password || "");
      if (r.ok) {
        ctx.setUser(r.body.user);
        for (const i of [cur, pw, pw2]) i.value = "";
        toast("Login changed; other sessions were signed out.");
      }
    });
    return card({ title: "Panel login", body: form });
  }

  function backupCard() {
    const exp = button("Export config");
    exp.addEventListener("click", async () => {
      const r = await verb("config.export");
      if (!r.ok) {
        toast(errorText(r), "bad");
        return;
      }
      const blob = new Blob([JSON.stringify(r.body.config, null, 2) + "\n"], { type: "application/json" });
      const a = h("a", { href: URL.createObjectURL(blob), download: "berth-config-r" + r.body.config.revision + ".json" });
      document.body.append(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(a.href), 1000);
    });
    const file = h("input", { type: "file", accept: "application/json,.json", hidden: true });
    const imp = button("Import config", { onclick: () => file.click() });
    file.addEventListener("change", async () => {
      const f = file.files[0];
      file.value = "";
      if (!f) return;
      let doc;
      try {
        doc = JSON.parse(await f.text());
      } catch (_) {
        toast("That file is not JSON.", "bad");
        return;
      }
      const cfg = doc && doc.config ? doc.config : doc;
      if (!cfg || cfg.schema !== 1) {
        toast("That file is not a Berth config export.", "bad");
        return;
      }
      const patch = Object.assign({}, cfg);
      delete patch.schema;
      delete patch.revision;
      const rows = [...PATHS, "agents.ref", "agents.disabled"]
        .filter((p) => getPath(cfg, p) !== undefined && JSON.stringify(getPath(cfg, p)) !== JSON.stringify(getPath(base, p)))
        .map((p) => h("li", null, h("span", { class: "mono", text: p }), ": ",
          JSON.stringify(getPath(base, p)), " → ", JSON.stringify(getPath(cfg, p))));
      dialog({
        title: "Import " + f.name + "?",
        body: rows.length ? [h("p", { text: "These settings change:" }), h("ul", null, rows),
          h("p", { class: "muted", text: "API keys are never part of an export, so yours stay as they are." })]
          : h("p", { text: "This file matches the current settings; nothing would change." }),
        actions: [
          { label: "Cancel", onclick: (close) => close() },
          { label: "Import", kind: "primary", onclick: async (close, b) => {
            if (!rows.length) {
              close();
              return;
            }
            const done = busy(b, "Importing…");
            const r = await verb("config.apply", { base_revision: base.revision, patch });
            done();
            if (!r.ok) {
              toast(errorText(r), "bad");
              return;
            }
            close();
            toast(describeChanges(r.body.changes));
            load();
          } },
        ],
      });
    });
    const revert = button("Revert to previous revision", { onclick: () => confirmDialog({
      title: "Revert to the previous settings?",
      body: "Berth applies the revision before the current one and restarts what it affects.",
      confirm: "Revert",
      action: async () => {
        const r = await verb("config.revert");
        if (!r.ok) {
          toast(errorText(r), "bad");
          return false;
        }
        toast(describeChanges(r.body.changes));
        load();
        ctx.refresh();
        return true;
      },
    }) });
    return card({ title: "Backup", body: h("p", { class: "muted", text: "Exports leave out API keys and the login." }),
      footer: [exp, imp, file, revert] });
  }

  async function load() {
    const r = await verb("config.get");
    if (!r.ok) {
      clear(formBox, alert("bad", errorText(r)));
      return;
    }
    base = r.body.config;
    draft = structuredClone(base);
    drawForm();
  }

  load();
  return { el };
}
