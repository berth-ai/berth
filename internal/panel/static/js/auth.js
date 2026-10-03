// Sign-in and the first-run setup wizard. The setup token arrives in the
// URL fragment, which never reaches a server or a log; it is read once,
// kept in memory, and removed from the address bar.
import { get, post, verb, errorText, fieldErrors } from "./api.js";
import { h, clear, button, busy, field, input, toggle, alert, card } from "./ui.js";
import { icon } from "./icons.js";

const root = document.getElementById("auth");

function logo() {
  return h("div", { class: "logo" }, h("span", { class: "logo-mark", "aria-hidden": "true" }), "Berth");
}

function lostLogin() {
  return h("p", { class: "muted small" }, "Lost the login? Run ",
    h("code", { text: "docker compose exec berth berth config reset-login" }), " and open the new setup link.");
}

async function loginPage() {
  const s = await get("/api/session");
  if (s.ok && s.body && s.body.user) {
    location.replace("/");
    return;
  }
  const user = input({ name: "user", autocomplete: "username", autofocus: true });
  const pass = input({ name: "password", type: "password", autocomplete: "current-password" });
  const fu = field({ label: "User", input: user });
  const fp = field({ label: "Password", input: pass });
  const status = h("div", { "aria-live": "polite" });
  const submit = button("Sign in", { kind: "primary", type: "submit" });
  const form = h("form", { id: "login-form", class: "form", novalidate: true }, fu.el, fp.el, status, submit);
  let timer = null;
  const lock = (seconds) => {
    let left = seconds;
    submit.disabled = true;
    clearInterval(timer);
    const tick = () => {
      if (left <= 0) {
        clearInterval(timer);
        clear(status);
        submit.disabled = false;
        return;
      }
      clear(status, alert("warn", "Too many attempts. Try again in " + left + " s."));
      left -= 1;
    };
    tick();
    timer = setInterval(tick, 1000);
  };
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    fu.setError(user.value.trim() ? "" : "Enter your user name.");
    fp.setError(pass.value ? "" : "Enter your password.");
    if (!user.value.trim() || !pass.value) return;
    clear(status);
    const done = busy(submit, "Signing in…");
    const r = await post("/api/login", { user: user.value.trim(), password: pass.value });
    done();
    if (r.ok) {
      location.replace("/");
      return;
    }
    pass.value = "";
    const retry = r.body && r.body.error && r.body.error.retry_after;
    if (r.status === 429 || retry > 2) {
      lock(retry || 1);
      return;
    }
    clear(status, alert("bad", r.status === 401 ? "Wrong user name or password." : errorText(r)));
  });
  clear(root, logo(), h("h1", { text: "Sign in" }), card({ body: form }), lostLogin());
}

const USER = /^[A-Za-z0-9._@-]{1,64}$/;

function strength(pw) {
  if (pw.length < 10) return "Too short: use at least 10 characters.";
  const kinds = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^A-Za-z0-9]/].filter((r) => r.test(pw)).length;
  if (pw.length >= 16 || kinds >= 3) return "Strong.";
  return "Acceptable. A longer passphrase is stronger.";
}

async function setupPage() {
  const token = new URLSearchParams(location.hash.slice(1)).get("token");
  history.replaceState(null, "", "/setup");
  const s = await get("/api/session");
  if (s.ok && s.body && s.body.mode && s.body.mode !== "setup") {
    linkError("Berth is already set up.", h("a", { class: "btn primary", href: "/login" }, "Sign in"));
    return;
  }
  if (!token) {
    linkError("This page needs the setup link from the container log.");
    return;
  }

  const steps = ["Login", "Frontends", "Extras", "Done"];
  const data = { user: "", password: "", terminal: true, ide: false, keys: 0, git: "" };
  let step = 0;
  const stepper = h("ol", { class: "stepper" });
  const title = h("h2");
  const count = h("span", { class: "muted" });
  const body = h("div", { class: "card-body" });
  const foot = h("div", { class: "card-foot" });
  const form = h("form", { id: "setup-form", class: "card", novalidate: true },
    h("div", { class: "card-head" }, h("div", { class: "wizard-title" }, title, count)), body, foot);
  clear(root, logo(), h("h1", { text: "Set up Berth" }), stepper, form);

  function draw() {
    clear(stepper, steps.map((label, i) => h("li", {
      class: i < step ? "done" : null, "aria-current": i === step ? "step" : null,
    }, h("span", { class: "dot" }, i < step ? icon("check", 12, 2) : String(i + 1)), label)));
    count.textContent = "Step " + (step + 1) + " of " + steps.length;
    [drawLogin, drawFrontends, drawExtras, drawDone][step]();
  }

  let onSubmit = null;
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    if (onSubmit) onSubmit();
  });

  function drawLogin() {
    title.textContent = "Create the panel login";
    const user = input({ autocomplete: "username", value: data.user });
    const pass = input({ type: "password", autocomplete: "new-password" });
    const again = input({ type: "password", autocomplete: "new-password" });
    const fu = field({ label: "User", input: user, hint: "Letters, digits and . _ @ -" });
    const fp = field({ label: "Password", input: pass, hint: "At least 10 characters." });
    const fa = field({ label: "Repeat password", input: again });
    pass.addEventListener("input", () => fp.setHint(strength(pass.value)));
    clear(body, fu.el, fp.el, fa.el);
    clear(foot, h("span", { class: "spacer" }), button("Next", { kind: "primary", type: "submit" }));
    user.focus();
    onSubmit = () => {
      const u = user.value.trim();
      fu.setError(USER.test(u) ? "" : "Use 1 to 64 letters, digits or . _ @ -");
      fp.setError(pass.value.length >= 10 ? "" : "Use at least 10 characters.");
      fa.setError(again.value === pass.value ? "" : "The passwords differ.");
      if (!USER.test(u) || pass.value.length < 10 || again.value !== pass.value) return;
      Object.assign(data, { user: u, password: pass.value });
      step = 1;
      draw();
    };
  }

  function drawFrontends() {
    title.textContent = "Choose the frontends";
    const term = toggle({ checked: data.terminal, label: "Terminal", onchange: (v) => { data.terminal = v; } });
    const ide = toggle({ checked: data.ide, label: "IDE", onchange: (v) => { data.ide = v; } });
    const status = h("div", { "aria-live": "polite" });
    const next = button("Create login", { kind: "primary", type: "submit" });
    clear(body,
      h("div", { class: "setting" }, h("div", null, h("strong", { text: "Terminal" }),
        h("span", { class: "muted", text: "A browser terminal with tmux; sessions survive closing the tab." })), term.el),
      h("div", { class: "setting" }, h("div", null, h("strong", { text: "IDE" }),
        h("span", { class: "muted", text: "VS Code in the browser (code-server)." })), ide.el),
      h("p", { class: "muted small", text: "You can change both later in Settings." }), status);
    clear(foot, button("Back", { onclick: () => { step = 0; draw(); } }), h("span", { class: "spacer" }), next);
    onSubmit = async () => {
      const done = busy(next, "Creating…");
      const r = await post("/api/setup", { token, user: data.user, password: data.password,
        patch: { frontends: { terminal: { enabled: data.terminal }, ide: { enabled: data.ide } } } });
      done();
      if (r.ok) {
        data.password = "";
        step = 2;
        draw();
        return;
      }
      const code = r.body && r.body.error && r.body.error.code;
      if (code === "invalid_token" || code === "not_in_setup") {
        linkError("This setup link is not valid or was already used.");
      } else if (code === "invalid_login") {
        step = 0;
        draw();
        body.prepend(alert("bad", errorText(r)));
      } else {
        clear(status, alert("bad", errorText(r)));
      }
    };
  }

  async function drawExtras() {
    title.textContent = "Optional extras";
    clear(body, h("p", { class: "muted", text: "Loading…" }));
    const st = await get("/api/status");
    const names = [];
    for (const p of (st.ok && st.body && st.body.packages) || []) for (const k of p.keys) names.push([k.name, p.name]);
    const keyInputs = names.map(([name, owner]) => {
      const inp = input({ type: "password", autocomplete: "off", spellcheck: "false" });
      return [name, field({ label: name, input: inp, mono: true, hint: "Used by " + owner + "." })];
    });
    const gname = input({ autocomplete: "name" });
    const gmail = input({ type: "email", autocomplete: "email" });
    const fn = field({ label: "Git name", input: gname });
    const fe = field({ label: "Git email", input: gmail });
    const status = h("div", { "aria-live": "polite" });
    const save = button("Save and continue", { kind: "primary", type: "submit" });
    clear(body,
      h("h3", { class: "section-title", text: "API keys" }),
      keyInputs.length ? keyInputs.map(([, f]) => f.el) : h("p", { class: "muted", text: "No installed agent needs a key yet." }),
      h("h3", { class: "section-title", text: "Git identity" }), h("div", { class: "form-row" }, fn.el, fe.el), status);
    clear(foot, h("span", { class: "spacer" }),
      button("Skip", { onclick: () => { step = 3; draw(); } }), save);
    onSubmit = async () => {
      clear(status);
      const done = busy(save, "Saving…");
      const secrets = {};
      for (const [name, f] of keyInputs) if (f.input.value) secrets[name] = f.input.value;
      let ok = true;
      if (Object.keys(secrets).length) {
        const r = await verb("config.apply", { secrets });
        const errs = fieldErrors(r);
        for (const [name, f] of keyInputs) f.setError(errs["secrets." + name] || "");
        if (!r.ok) {
          ok = false;
          if (!Object.keys(errs).length) clear(status, alert("bad", errorText(r)));
        } else data.keys = Object.keys(secrets).length;
      }
      if (ok && (gname.value.trim() || gmail.value.trim())) {
        const r = await verb("git.identity", { name: gname.value.trim(), email: gmail.value.trim() });
        if (!r.ok) {
          ok = false;
          clear(status, alert("bad", errorText(r)));
        } else data.git = gname.value.trim() + " <" + gmail.value.trim() + ">";
      }
      done();
      if (ok) {
        step = 3;
        draw();
      }
    };
  }

  function drawDone() {
    title.textContent = "Berth is ready";
    const rows = [
      ["Panel login", data.user],
      ["Terminal", data.terminal ? "On" : "Off"],
      ["IDE", data.ide ? "On" : "Off"],
      ["API keys", data.keys ? data.keys + " saved" : "None yet"],
      ["Git identity", data.git || "Not set"],
    ];
    clear(body, h("dl", { class: "facts" }, rows.map(([k, v]) => [h("dt", { text: k }), h("dd", { text: v })])),
      h("p", { class: "muted", text: "The first start installs the agent CLIs, which can take a few minutes. The overview shows when they are ready." }));
    clear(foot, h("span", { class: "spacer" }), button("Open Berth", { kind: "primary", type: "submit" }));
    onSubmit = () => location.replace("/");
  }

  draw();
}

function linkError(message, action) {
  clear(root, logo(), h("h1", { text: "Set up Berth" }), card({
    body: [
      alert("bad", message),
      h("p", null, "Find a fresh link with ", h("code", { text: "docker compose logs berth | grep setup" }),
        ". Restarting the container in setup mode prints a new one."),
    ],
    footer: action || null,
  }));
}

if (root.dataset.page === "login") loginPage();
else setupPage();
