// Berth sign-in and first-run setup. The setup token arrives in the URL
// fragment, which never reaches a server or a log. It is read once, kept in
// memory, and removed from the address bar.
"use strict";

const $ = (id) => document.getElementById(id);

async function post(path, body) {
  let res;
  try {
    res = await fetch(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json", "X-Berth-Panel": "1" },
      body: JSON.stringify(body),
    });
  } catch (err) {
    return { ok: false, status: 0, body: { error: { message: "the panel is not reachable: " + err.message } } };
  }
  let data = null;
  try {
    data = await res.json();
  } catch (_) {
    data = null;
  }
  return { ok: res.ok, status: res.status, body: data };
}

function showError(message) {
  $("error").textContent = message || "";
  $("error").hidden = !message;
}

function describe(r) {
  const e = (r.body && r.body.error) || {};
  if (e.fields && e.fields.length) return e.fields.map((f) => f.message).join(" ");
  if (e.retry_after) return (e.message || "Try again later.") + " Try again in " + e.retry_after + " s.";
  return e.message || "HTTP " + r.status;
}

function busy(on) {
  $("submit").disabled = on;
}

function initLogin() {
  $("login-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    busy(true);
    const r = await post("/api/login", { user: $("user").value.trim(), password: $("password").value });
    busy(false);
    if (r.ok) {
      location.replace("/");
      return;
    }
    $("password").value = "";
    showError(describe(r));
  });
}

function initSetup() {
  const token = new URLSearchParams(location.hash.slice(1)).get("token");
  history.replaceState(null, "", "/setup");
  if (!token) {
    $("no-token").hidden = false;
    busy(true);
    return;
  }
  $("setup-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    if ($("password").value !== $("password2").value) {
      showError("The passwords differ.");
      return;
    }
    busy(true);
    const r = await post("/api/setup", {
      token,
      user: $("user").value.trim(),
      password: $("password").value,
      patch: { frontends: { terminal: { enabled: $("terminal").checked }, ide: { enabled: $("ide").checked } } },
    });
    busy(false);
    if (r.ok) {
      location.replace("/");
      return;
    }
    showError(describe(r));
  });
}

document.addEventListener("DOMContentLoaded", () => {
  if ($("login-form")) initLogin();
  if ($("setup-form")) initSetup();
});
