// Berth's shared components (DESIGN.md §4). Everything is built from DOM
// nodes; text from the server goes in as text, never as markup.
import { icon, statusIcon } from "./icons.js";

/** h builds an element. Props: class, text, on<event>, boolean or string attributes. */
export function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === "class") el.className = v;
    else if (k === "text") el.textContent = v;
    else if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? "" : String(v));
  }
  append(el, kids);
  return el;
}

function append(el, kids) {
  for (const kid of kids.flat(Infinity)) {
    if (kid === null || kid === undefined || kid === false) continue;
    el.append(kid instanceof Node ? kid : String(kid));
  }
}

/** clear replaces el's children. */
export function clear(el, ...kids) {
  el.replaceChildren();
  append(el, kids);
  return el;
}

// Status vocabulary: tone, icon, word (§4: never colour alone).
const STATES = {
  running: ["ok", "check", "Running"],
  succeeded: ["ok", "check", "Succeeded"],
  installed: ["ok", "check", "Installed"],
  signed_in: ["ok", "check", "Signed in"],
  set: ["ok", "check", "Set"],
  on: ["ok", "check", "On"],
  starting: ["warn", "arrow", "Starting"],
  restarting: ["warn", "arrow", "Restarting"],
  outdated: ["warn", "up", "Update available"],
  update: ["warn", "up", "Update available"],
  in_progress: ["info", "arrow", "Running"],
  waiting: ["info", "clock", "Waiting"],
  failed: ["bad", "cross", "Failed"],
  stopped: ["idle", "square", "Stopped"],
  disabled: ["idle", "dash", "Disabled"],
  off: ["idle", "dash", "Off"],
  not_set: ["idle", "dash", "Not set"],
  signed_out: ["idle", "dash", "Signed out"],
  not_installed: ["idle", "dash", "Not installed"],
  unknown: ["idle", "dash", "Unknown"],
};

export function badge(state, detail, small) {
  const [tone, mark, word] = STATES[state] || STATES.unknown;
  return h("span", { class: "badge " + tone + (small ? " small" : "") },
    statusIcon(mark, small ? 11 : 12), word,
    detail ? h("span", { class: "badge-detail", text: detail }) : null);
}

export function button(label, opts = {}) {
  return h("button", {
    type: opts.type || "button",
    class: "btn " + (opts.kind || "secondary") + (opts.compact ? " compact" : ""),
    onclick: opts.onclick, disabled: opts.disabled, "aria-label": opts.ariaLabel,
  }, opts.icon ? icon(opts.icon, opts.compact ? 13 : 14, 1.6) : null, label ? h("span", { text: label }) : null);
}

/** busy shows a spinner and a verb on b, keeping its width, until the returned function runs. */
export function busy(b, verbText) {
  const kids = [...b.childNodes];
  b.style.minWidth = b.offsetWidth + "px";
  b.disabled = true;
  b.setAttribute("aria-busy", "true");
  b.replaceChildren(h("span", { class: "spinner", "aria-hidden": "true" }), h("span", { text: verbText }));
  return () => {
    b.replaceChildren(...kids);
    b.disabled = false;
    b.removeAttribute("aria-busy");
    b.style.minWidth = "";
  };
}

/** toggle is a role=switch with its On/Off word to the left. */
export function toggle({ checked, label, onchange, disabled, id }) {
  const word = h("span", { class: "toggle-word", text: checked ? "On" : "Off" });
  const sw = h("button", { type: "button", class: "toggle", role: "switch", id, disabled,
    "aria-checked": String(!!checked), "aria-label": label }, h("span", { class: "toggle-knob" }));
  const set = (v) => {
    sw.setAttribute("aria-checked", String(!!v));
    word.textContent = v ? "On" : "Off";
  };
  sw.addEventListener("click", () => {
    const v = sw.getAttribute("aria-checked") !== "true";
    set(v);
    if (onchange) onchange(v);
  });
  return { el: h("span", { class: "toggle-wrap" }, word, sw), set, get: () => sw.getAttribute("aria-checked") === "true", button: sw };
}

let seq = 0;
export function nextId(prefix) {
  seq += 1;
  return (prefix || "id") + seq;
}

export function input(props) {
  return h("input", Object.assign({ class: "input" }, props));
}

export function select(options, value) {
  const s = h("select", { class: "input" });
  for (const [v, label] of options) s.append(h("option", { value: v, text: label, selected: v === value }));
  return s;
}

/** field labels an input, with a hint and an error tied to it by aria-describedby. */
export function field({ label, hint, input: inp, mono }) {
  const id = inp.id || nextId("f");
  inp.id = id;
  if (mono) inp.classList.add("mono");
  const hintEl = h("p", { class: "hint", id: id + "-hint", text: hint || "", hidden: !hint });
  const err = h("p", { class: "field-error", id: id + "-err", hidden: true });
  inp.setAttribute("aria-describedby", id + "-hint " + id + "-err");
  return {
    el: h("div", { class: "field" }, h("label", { for: id, text: label }), inp, hintEl, err),
    input: inp,
    setHint(text) {
      hintEl.textContent = text || "";
      hintEl.hidden = !text;
    },
    setError(msg) {
      err.replaceChildren();
      if (msg) err.append(icon(["circle", "alert"], 12, 2), msg);
      err.hidden = !msg;
      if (msg) inp.setAttribute("aria-invalid", "true");
      else inp.removeAttribute("aria-invalid");
    },
  };
}

/** secretField is write-only: once saved, a key is only ever "Set". */
export function secretField({ name, set, onSave, onRemove }) {
  const el = h("div", { class: "secret" });
  const showSet = () => clear(el,
    h("span", { class: "mono", text: name }), badge("set"),
    button("Replace", { compact: true, onclick: showEdit }),
    button("Remove", { compact: true, onclick: () => confirmDialog({
      title: "Remove " + name + "?",
      body: "Agents that read this key stop working with it until you set it again.",
      confirm: "Remove key", danger: true,
      action: async () => {
        const err = await onRemove();
        if (err) {
          toast(err, "bad");
          return false;
        }
        showEdit();
        return true;
      },
    }) }));
  function showEdit() {
    const inp = input({ type: "password", autocomplete: "off", spellcheck: "false", placeholder: "Paste the key" });
    const f = field({ label: name, input: inp, mono: true });
    const save = button("Save", { compact: true, kind: "primary" });
    save.addEventListener("click", async () => {
      if (!inp.value) {
        f.setError("Paste a key first.");
        return;
      }
      const done = busy(save, "Saving…");
      const err = await onSave(inp.value);
      done();
      if (err) f.setError(err);
      else showSet();
    });
    clear(el, h("div", { class: "secret-edit" }, f.el, save));
  }
  if (set) showSet();
  else showEdit();
  return el;
}

const FOCUSABLE = 'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea, a[href], [tabindex]:not([tabindex="-1"])';

/** dialog opens an in-page modal; actions get (close, button). Returns close. */
export function dialog({ title, body, actions }) {
  const prev = document.activeElement;
  const titleId = nextId("dlg");
  const foot = h("div", { class: "dialog-foot" });
  const panel = h("div", { class: "dialog", role: "dialog", "aria-modal": "true", "aria-labelledby": titleId },
    h("div", { class: "dialog-head" }, h("h2", { id: titleId, text: title })),
    h("div", { class: "dialog-body" }, body), foot);
  const scrim = h("div", { class: "scrim" }, panel);
  let closed = false;
  function close() {
    if (closed) return;
    closed = true;
    scrim.remove();
    document.removeEventListener("keydown", onKey, true);
    if (prev && prev.focus) prev.focus();
  }
  function onKey(e) {
    if (e.key === "Escape" && !panel.querySelector("[aria-busy]")) {
      e.preventDefault();
      close();
    } else if (e.key === "Tab") {
      const f = [...panel.querySelectorAll(FOCUSABLE)];
      if (!f.length) return;
      const first = f[0];
      const last = f[f.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }
  for (const a of actions) {
    const b = button(a.label, { kind: a.kind || "secondary" });
    b.addEventListener("click", () => a.onclick(close, b));
    foot.append(b);
  }
  scrim.addEventListener("click", (e) => {
    if (e.target === scrim && !panel.querySelector("[aria-busy]")) close();
  });
  document.addEventListener("keydown", onKey, true);
  document.body.append(scrim);
  (panel.querySelector(".dialog-body input, .dialog-body select") || foot.lastElementChild).focus();
  return close;
}

/** confirmDialog asks before an action; action returns false to stay open. */
export function confirmDialog({ title, body, confirm, danger, action }) {
  return dialog({
    title,
    body: typeof body === "string" ? h("p", { text: body }) : body,
    actions: [
      { label: "Cancel", onclick: (close) => close() },
      {
        label: confirm, kind: danger ? "danger" : "primary",
        onclick: async (close, b) => {
          const done = busy(b, confirm + "…");
          const keep = (await action()) === false;
          done();
          if (!keep) close();
        },
      },
    ],
  });
}

const TONE_ICON = { ok: "check", bad: "cross", warn: "alert", info: "clock" };

/** toast confirms briefly; errors stay until dismissed. */
export function toast(message, tone = "ok") {
  const box = document.getElementById("toasts");
  if (!box) return;
  const t = h("div", { class: "toast " + tone, role: "status" },
    statusIcon(TONE_ICON[tone] || "check", 16), h("span", { class: "toast-msg", text: message }),
    h("button", { type: "button", class: "icon-btn", "aria-label": "Dismiss", onclick: () => t.remove() }, icon("close", 14, 1.6)));
  box.append(t);
  if (tone !== "bad") setTimeout(() => t.remove(), 5000);
}

/** alert is an inline alert: icon, sentence, optional action. */
export function alert(tone, text, action) {
  return h("div", { class: "alert " + tone, role: tone === "bad" ? "alert" : "status" },
    statusIcon(TONE_ICON[tone] || "check", 16), h("span", { text }), action || null);
}

/** logViewer shows a job log with Follow and Copy. */
export function logViewer() {
  const pre = h("pre", { class: "log", role: "log", tabindex: "0" });
  let follow = true;
  const followToggle = toggle({ checked: true, label: "Follow the log", onchange: (v) => {
    follow = v;
    if (v) pre.scrollTop = pre.scrollHeight;
  } });
  const copy = button("Copy", { compact: true, icon: "copy", onclick: async () => {
    try {
      await navigator.clipboard.writeText(pre.textContent);
      toast("Log copied.");
    } catch (_) {
      toast("Copy failed; select the text instead.", "bad");
    }
  } });
  const el = h("div", { class: "card log-card" },
    h("div", { class: "log-toolbar" }, h("span", { class: "muted", text: "Follow" }), followToggle.el, copy), pre);
  return {
    el,
    set(text, truncated) {
      pre.replaceChildren();
      if (truncated) pre.append(h("span", { class: "log-dim", text: "… earlier output truncated …\n" }));
      for (const line of (text || "").replace(/\n$/, "").split("\n")) {
        pre.append(/\b(error|failed|fatal)\b/i.test(line) ? h("span", { class: "log-err", text: line }) : line, "\n");
      }
      if (follow) pre.scrollTop = pre.scrollHeight;
    },
  };
}

/** saveBar is the sticky "Unsaved changes" region. */
export function saveBar({ onSave, onDiscard }) {
  const summary = h("span", { class: "save-summary" });
  const save = button("Save", { kind: "primary" });
  save.addEventListener("click", () => onSave(save));
  const el = h("div", { class: "save-bar", role: "region", "aria-label": "Unsaved changes", hidden: true },
    h("strong", { text: "Unsaved changes" }), summary, h("span", { class: "spacer" }),
    button("Discard", { onclick: onDiscard }), save);
  return {
    el,
    show(text) {
      summary.textContent = text;
      el.hidden = false;
    },
    hide() {
      el.hidden = true;
    },
  };
}

export function segmented(options, value, onchange, label) {
  const el = h("div", { class: "segmented", role: "group", "aria-label": label });
  const buttons = options.map(([v, text]) => h("button", { type: "button", "aria-pressed": String(v === value), text }));
  buttons.forEach((b, i) => b.addEventListener("click", () => {
    for (const x of buttons) x.setAttribute("aria-pressed", String(x === b));
    onchange(options[i][0]);
  }));
  el.append(...buttons);
  return el;
}

export function card({ title, badge: b, actions, body, footer, cls }) {
  return h("section", { class: "card" + (cls ? " " + cls : "") },
    title ? h("div", { class: "card-head" }, h("h2", { class: "card-title", text: title }), b || null,
      h("span", { class: "spacer" }), actions || []) : null,
    body ? h("div", { class: "card-body" }, body) : null,
    footer ? h("div", { class: "card-foot" }, footer) : null);
}

export function pageHeader(title, description, ...actions) {
  return h("header", { class: "page-head" },
    h("div", null, h("h1", { text: title }), description ? h("p", { class: "muted", text: description }) : null),
    h("div", { class: "page-actions" }, actions));
}

export function empty(text, action) {
  return h("div", { class: "empty" }, h("p", { text }), action || null);
}

export function skeleton(n = 3) {
  return h("div", { class: "skeleton-group", "aria-busy": "true" }, Array.from({ length: n }, () => h("div", { class: "skeleton" })));
}

/** table renders columns [{label, num}] and rows of cells (strings or nodes). */
export function table(columns, rows) {
  return h("div", { class: "table-wrap" }, h("table", null,
    h("thead", null, h("tr", null, columns.map((c) => h("th", { class: c.num ? "num" : null, text: c.label })))),
    h("tbody", null, rows.map((cells) => h("tr", null, cells.map((cell, i) =>
      h("td", { class: columns[i].num ? "num" : null, "data-label": columns[i].label || null }, cell)))))));
}

export function facts(pairs) {
  return h("dl", { class: "facts" }, pairs.map(([k, v]) => [h("dt", { text: k }), h("dd", null, v)]));
}

// Formatting.
export function when(iso) {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

function span(seconds) {
  if (seconds < 60) return Math.max(0, Math.floor(seconds)) + " s";
  if (seconds < 3600) return Math.floor(seconds / 60) + " min";
  if (seconds < 86400) return Math.floor(seconds / 3600) + " h " + Math.floor((seconds % 3600) / 60) + " min";
  return Math.floor(seconds / 86400) + " d " + Math.floor((seconds % 86400) / 3600) + " h";
}

/** since is the time elapsed since iso, as "3 h 12 min". */
export function since(iso) {
  return iso ? span((Date.now() - new Date(iso).getTime()) / 1000) : "—";
}

export function duration(from, to) {
  if (!from) return "—";
  const end = to ? new Date(to).getTime() : Date.now();
  return span((end - new Date(from).getTime()) / 1000);
}

export function short(hash) {
  return hash ? hash.slice(0, 8) : "";
}

export function describeSource(s) {
  if (!s) return "unknown";
  if (s.origin === "image") return "image";
  if (s.origin === "ref") return "github:" + (s.ref || "?") + (s.commit ? " (" + s.commit.slice(0, 7) + ")" : "");
  if (s.origin === "dir") return "local directory";
  return s.origin || "unknown";
}

export const SERVICE_LABELS = { panel: "Panel", ttyd: "Terminal", code_server: "IDE" };

/** describeChanges says what a config change did to the services. */
export function describeChanges(ch) {
  if (!ch) return "Saved.";
  const name = (n) => (SERVICE_LABELS[n] || n).toLowerCase();
  const parts = [];
  if (ch.started && ch.started.length) parts.push("started the " + ch.started.map(name).join(" and the "));
  if (ch.restarted && ch.restarted.length) parts.push("restarted the " + ch.restarted.map(name).join(" and the "));
  if (ch.stopped && ch.stopped.length) parts.push("stopped the " + ch.stopped.map(name).join(" and the "));
  if (!parts.length) return "Saved; nothing restarted.";
  const text = parts.join("; ");
  return "Saved; " + text + ".";
}
