// The panel shell: navigation, theme, banners, status polling and routing.
import { get, post, onConnection } from "./api.js";
import { icon, statusIcon } from "./icons.js";
import { h, clear, button, segmented, skeleton, short } from "./ui.js";
import { startJobs, onRunning, jobTitle } from "./jobs.js";
import * as overview from "./pages/overview.js";
import { terminal, ide } from "./pages/frontend.js";
import * as agents from "./pages/agents.js";
import * as tools from "./pages/tools.js";
import * as accounts from "./pages/accounts.js";
import * as settings from "./pages/settings.js";
import * as activity from "./pages/activity.js";
import * as recovery from "./pages/recovery.js";

const NAV = [
  ["overview", "Overview", overview],
  ["terminal", "Terminal", terminal],
  ["ide", "IDE", ide],
  ["agents", "Agents", agents],
  ["tools", "Tools", tools],
  ["accounts", "Accounts", accounts],
  ["settings", "Settings", settings],
  ["activity", "Activity", activity],
];
const THEME_KEY = "berth-theme";

function storedTheme() {
  try {
    return localStorage.getItem(THEME_KEY) || "system";
  } catch (_) {
    return "system";
  }
}

function applyTheme(theme) {
  if (theme === "light" || theme === "dark") document.documentElement.dataset.theme = theme;
  else delete document.documentElement.dataset.theme;
}
applyTheme(storedTheme());

const state = { status: null, session: { user: null, mode: "" }, listeners: new Set(), page: null, offline: false };

const ctx = {
  status: () => state.status,
  onStatus(fn) {
    state.listeners.add(fn);
    if (state.status) fn(state.status);
    return () => state.listeners.delete(fn);
  },
  refresh: () => loadStatus(),
  navigate(hash) {
    location.hash = hash;
  },
  session: () => state.session,
  setUser(name) {
    state.session.user = name;
    userName.textContent = name;
  },
};

// Navigation.
const nav = document.getElementById("nav");
const topbar = document.getElementById("topbar");
const navLinks = {};
const failCount = h("span", { class: "nav-count", hidden: true });
const version = h("span", { class: "version" });
const userName = h("span");
const jobSlot = h("div");

function logo(withVersion) {
  return h("a", { class: "logo", href: "#overview", "aria-label": "Berth overview" },
    h("span", { class: "logo-mark", "aria-hidden": "true" }), "Berth", withVersion ? version : null);
}

const menuButton = h("button", { type: "button", class: "icon-btn menu", "aria-label": "Menu", "aria-expanded": "false", "aria-controls": "nav" }, icon("menu", 18, 1.6));
menuButton.addEventListener("click", () => setMenu(!nav.classList.contains("open")));
function setMenu(open) {
  nav.classList.toggle("open", open);
  menuButton.setAttribute("aria-expanded", String(open));
}
clear(topbar, menuButton, logo(false));

nav.append(logo(true));
for (const [name, label] of NAV) {
  const a = h("a", { class: "nav-item", href: "#" + name }, icon(name), h("span", { text: label }), name === "overview" ? failCount : null);
  navLinks[name] = a;
  nav.append(a);
}
nav.append(h("div", { class: "nav-bottom" }, jobSlot,
  segmented([["light", "Light"], ["dark", "Dark"], ["system", "System"]], storedTheme(), (t) => {
    try {
      localStorage.setItem(THEME_KEY, t);
    } catch (_) {
      // Private windows may refuse storage; the theme still applies now.
    }
    applyTheme(t);
  }, "Theme"),
  h("div", { class: "user-row" }, icon("accounts", 14), userName, h("span", { class: "spacer" }),
    button("Sign out", { kind: "quiet", compact: true, onclick: async () => {
      await post("/api/logout", {});
      location.replace("/login");
    } }))));

onRunning((jobs) => {
  if (!jobs.length) {
    clear(jobSlot);
    return;
  }
  const first = jobs[0];
  clear(jobSlot, h("a", { class: "job-indicator", href: "#activity/" + first.id },
    h("span", { class: "label" }, h("span", { class: "spinner", "aria-hidden": "true" }),
      jobs.length > 1 ? jobs.length + " jobs running" : jobTitle(first) + "…"),
    h("span", { class: "progress", "aria-hidden": "true" }, h("span")),
    h("span", { text: "View log" })));
});

// Banners.
const banners = document.getElementById("banners");
function renderBanners() {
  const list = [];
  if (state.offline) {
    list.push(h("div", { class: "banner bad", role: "alert" }, statusIcon("cross", 16), h("strong", { text: "Disconnected" }),
      h("span", { text: "The panel is not reachable. It may be restarting; this page retries on its own." })));
  }
  const check = state.status && state.status.check;
  if (check && check.update_available && location.hash !== "#agents") {
    list.push(h("div", { class: "banner warn", role: "status" }, statusIcon("up", 16), h("strong", { text: "Update available" }),
      h("span", { text: "A newer agent set is on " + check.ref + " (" + short(check.hash) + ")." }),
      h("a", { class: "btn secondary compact", href: "#agents" }, "Review")));
  }
  clear(banners, list);
}
onConnection((online) => {
  state.offline = !online;
  renderBanners();
});

// Status.
let lastMode = "";
async function loadStatus() {
  const r = await get("/api/status");
  if (!r.ok || !r.body) return;
  state.status = r.body;
  const st = r.body;
  const failed = (st.services || []).filter((s) => s.state === "failed").length;
  failCount.hidden = !failed;
  clear(failCount, failed ? [statusIcon("cross", 11), String(failed)] : []);
  const build = st.agents && st.agents.build;
  version.textContent = build && build.version ? build.version : "";
  for (const fn of state.listeners) fn(st);
  renderBanners();
  if (st.mode !== lastMode) {
    const wasRecovery = lastMode === "recovery";
    lastMode = st.mode;
    if (wasRecovery || st.mode === "recovery") route();
  }
}

// Router.
function route() {
  const [name, param] = (location.hash.slice(1) || "overview").split("/");
  const entry = NAV.find(([n]) => n === name) || NAV[0];
  if (state.page && state.page.dispose) state.page.dispose();
  state.listeners.clear();
  for (const [n, a] of Object.entries(navLinks)) {
    if (n === entry[0]) a.setAttribute("aria-current", "page");
    else a.removeAttribute("aria-current");
  }
  const main = document.getElementById("page");
  if (state.status && state.status.mode === "recovery") {
    state.page = recovery.render(ctx);
    document.title = "Recovery · Berth";
  } else {
    state.page = entry[2].render(ctx, param);
    document.title = entry[1] + " · Berth";
  }
  clear(main, state.page.el);
  main.focus({ preventScroll: true });
  setMenu(false);
  renderBanners();
}

async function boot() {
  clear(document.getElementById("page"), h("div", { class: "page" }, skeleton(6)));
  const s = await get("/api/session");
  if (s.ok && s.body) {
    state.session = s.body;
    userName.textContent = s.body.user || "";
  }
  await loadStatus();
  lastMode = state.status ? state.status.mode : "";
  route();
  window.addEventListener("hashchange", route);
  setInterval(loadStatus, 5000);
  startJobs();
}

boot();
