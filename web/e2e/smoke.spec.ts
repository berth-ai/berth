import { expect, test, type Page } from "@playwright/test";

const NORMAL = "http://127.0.0.1:7691";
const SETUP = "http://127.0.0.1:7692";
const RECOVERY = "http://127.0.0.1:7693";

// Expected by design: the terminal and IDE proxies answer 502, a failed
// sign-in 401, a vanished job 404, and a save on a stale revision 409.
const EXPECTED = /Failed to load resource: the server responded with a status of (401|404|409|502)/;

/** watch collects CSP violations, console errors and uncaught errors. */
async function watch(page: Page): Promise<string[]> {
  const problems: string[] = [];
  await page.addInitScript(() => {
    document.addEventListener("securitypolicyviolation", (e) => {
      console.error(`CSP violation: ${e.violatedDirective} ${e.blockedURI} in ${e.sourceFile}`);
    });
  });
  page.on("console", (m) => {
    if (m.type() === "error" && !EXPECTED.test(m.text())) problems.push(m.text());
  });
  page.on("pageerror", (e) => problems.push(e.message));
  return problems;
}

async function signIn(page: Page, base = NORMAL) {
  await page.goto(`${base}/login`);
  await page.getByLabel("User").fill("dev");
  await page.getByLabel("Password").fill("dev password");
  await page.getByRole("button", { name: "Sign in" }).click();
  // The nav collapses behind "Menu" below 900 px; main shows at every width.
  await expect(page.getByRole("main")).toBeVisible();
}

const h1 = (page: Page, name: string) => page.getByRole("heading", { level: 1, name, exact: true });

test("a wrong password says so", async ({ page }) => {
  const problems = await watch(page);
  await page.goto(`${NORMAL}/login`);
  await page.getByLabel("User").fill("dev");
  await page.getByLabel("Password").fill("wrong password");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("Wrong user name or password.")).toBeVisible();
  await expect(page.getByLabel("Password")).toHaveValue("");
  await page.waitForTimeout(1100); // the limiter's first backoff, before later sign-ins
  expect(problems).toEqual([]);
});

test("every page renders inside the CSP", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  for (const [hash, heading] of [
    ["#/", "Overview"],
    ["#/workspaces", "Workspaces"],
    ["#/agents", "Agents"],
    ["#/mcp", "MCP servers"],
    ["#/skills", "Skills"],
    ["#/tools", "Tools"],
    ["#/tools?tab=yours", "Tools"],
    ["#/tools?tab=presets", "Tools"],
    ["#/accounts", "Accounts"],
    ["#/settings", "Settings"],
    ["#/activity", "Activity"],
  ] as const) {
    await page.goto(`${NORMAL}/${hash}`);
    await expect(h1(page, heading)).toBeVisible();
  }
  await page.goto(`${NORMAL}/#/terminal`);
  await expect(
    page.frameLocator('iframe[title="Terminal"]').getByText("The terminal is not running"),
  ).toBeVisible();
  // A right click in the terminal goes to tmux, not the browser menu.
  const rightClick = (shiftKey: boolean) =>
    page
      .frameLocator('iframe[title="Terminal"]')
      .locator("body")
      .evaluate(
        (body, shiftKey) =>
          !body.dispatchEvent(
            new MouseEvent("contextmenu", { bubbles: true, cancelable: true, shiftKey }),
          ),
        shiftKey,
      );
  await expect.poll(() => rightClick(false)).toBe(true);
  expect(await rightClick(true)).toBe(false);
  await page.goto(`${NORMAL}/#/ide`);
  await expect(page.getByText("The IDE is off")).toBeVisible();
  await page.getByRole("button", { name: "Dark", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.getByRole("button", { name: "System", exact: true }).click();
  expect(problems).toEqual([]);
});

test("a job runs to completion and shows its log", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page
    .getByRole("status")
    .filter({ hasText: "Agent update available" })
    .getByRole("button", { name: "Update" })
    .click();
  await expect(page.getByRole("link", { name: /Agent update…/ })).toBeVisible();
  await expect(page.getByText("Agents updated.")).toBeVisible({ timeout: 15_000 });
  await page.getByRole("link", { name: "Activity", exact: true }).click();
  await page.getByRole("link", { name: "Agent update", exact: true }).first().click();
  await expect(h1(page, "Agent update")).toBeVisible();
  await expect(page.getByRole("log")).toContainText("done");
  await expect(page.getByText("Succeeded")).toBeVisible();
  expect(problems).toEqual([]);
});

test("M3 locations still open their pages", async ({ page }) => {
  await signIn(page);
  await page.goto(`${NORMAL}/#settings`);
  await expect(h1(page, "Settings")).toBeVisible();
  await page.evaluate(() => {
    window.location.hash = "agents";
  });
  await expect(h1(page, "Agents")).toBeVisible();
  await page.goto("about:blank");
  await page.goto(`${NORMAL}/#activity/1`);
  await expect(page).toHaveURL(/#\/activity\/1$/);
});

test("dialogs, selects, autocompletes and toasts stay inside the CSP", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page.goto(`${NORMAL}/#/tools?tab=yours`);
  await page.getByRole("button", { name: "Add tool" }).first().click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Tool").fill("terr");
  await page.getByRole("option", { name: "terraform" }).click();
  await dialog.getByLabel("Version").click();
  await page.getByRole("option", { name: "1.0.0" }).click();
  await dialog.getByRole("button", { name: "Add tool" }).click();
  await expect(page.getByText("terraform 1.0.0 installed.")).toBeVisible({ timeout: 15_000 });
  expect(problems).toEqual([]);
});

test("a user tool upgrades in place and an MCP server switches off", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  // An unknown tab falls back to Built-in.
  await page.goto(`${NORMAL}/#/tools?tab=junk`);
  await expect(page.getByRole("table", { name: "Built-in tools" })).toBeVisible();

  await page.goto(`${NORMAL}/#/tools?tab=yours`);
  const row = page.getByRole("row").filter({ hasText: "terraform" });
  await expect(row.getByText("Update available")).toBeVisible();
  await row.getByRole("button", { name: "Upgrade" }).click();
  await expect(page.getByText("terraform 2.0.0 installed.")).toBeVisible({ timeout: 15_000 });
  await expect(row.getByText("Installed", { exact: true })).toBeVisible();
  await expect(row.getByRole("button", { name: "Upgrade" })).toHaveCount(0);

  await page.goto(`${NORMAL}/#/mcp`);
  const mcp = page.getByRole("row").filter({ hasText: "Files MCP" });
  await expect(mcp.getByText("None needed")).toBeVisible();
  await expect(mcp.getByRole("button", { name: /Manage/ })).toHaveCount(0);
  await mcp.getByRole("switch", { name: "Turn off Files MCP" }).click();
  await expect(page.getByText(/Files MCP is off\./)).toBeVisible();
  await expect(mcp.getByRole("switch", { name: "Turn on Files MCP" })).not.toBeChecked();
  await mcp.getByRole("switch", { name: "Turn on Files MCP" }).click();
  await expect(page.getByText(/Files MCP is on\./)).toBeVisible();

  await page.goto(`${NORMAL}/#/agents`);
  const agent = page.getByRole("row").filter({ hasText: "Example Agent" });
  await agent.getByRole("button", { name: "Manage Example Agent" }).click();
  await expect(page.getByLabel("EXAMPLE_API_KEY")).toBeVisible();
  await page.getByRole("button", { name: "Package versions for 3f9c2a7d" }).click();
  await expect(page.getByText("@example/agent-cli").first()).toBeVisible();
  expect(problems).toEqual([]);
});

test("nothing scrolls sideways at 400 px", async ({ page }) => {
  await page.setViewportSize({ width: 400, height: 800 });
  await signIn(page);
  for (const [hash, text] of [
    ["#/", "Overview"],
    ["#/workspaces", "Workspaces"],
    ["#/agents", "Agents"],
    ["#/mcp", "MCP servers"],
    ["#/skills", "Skills"],
    ["#/tools", "Tools"],
    ["#/tools?tab=yours", "Your tools"],
    ["#/accounts", "Accounts"],
    ["#/settings", "Settings"],
    ["#/activity", "Activity"],
    ["#/terminal", "Open in new tab"],
  ] as const) {
    await page.goto(`${NORMAL}/${hash}`);
    await expect(
      page.getByText(text, { exact: true }).filter({ visible: true }).first(),
    ).toBeVisible();
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    );
    expect(overflow, hash).toBeLessThanOrEqual(0);
  }
  await page.getByRole("button", { name: "Menu" }).click();
  await expect(page.getByRole("link", { name: "Settings", exact: true })).toBeVisible();
});

test("a conflicting save keeps this tab's edit and the other change", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page.goto(`${NORMAL}/#/settings`);
  await page.getByRole("switch", { name: "IDE" }).click();
  await expect(page.getByRole("region", { name: "Unsaved changes" })).toContainText(
    "Starts the IDE.",
  );
  // Another tab changes the time zone meanwhile.
  const headers = { "X-Berth-Panel": "1" };
  const config = async () =>
    (
      await (
        await page.request.post(`${NORMAL}/api/verbs/config.get`, { data: {}, headers })
      ).json()
    ).config;
  const other = await page.request.post(`${NORMAL}/api/verbs/config.apply`, {
    data: { base_revision: (await config()).revision, patch: { runtime: { tz: "Europe/Rome" } } },
    headers,
  });
  expect(other.ok()).toBe(true);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText(/Settings changed elsewhere/)).toBeVisible();
  await expect(page.getByRole("switch", { name: "IDE" })).toBeChecked();
  await expect(page.getByLabel("Time zone")).toHaveValue("Europe/Rome");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Saved; started the ide.")).toBeVisible();
  const after = await config();
  expect(after.frontends.ide.enabled).toBe(true);
  expect(after.runtime.tz).toBe("Europe/Rome");
  expect(problems).toEqual([]);
});

test("the setup wizard creates the login", async ({ page }) => {
  const problems = await watch(page);
  // The link pasted into a tab already on /setup changes only the fragment.
  await page.goto(`${SETUP}/setup`);
  await expect(page.getByText(/link/i).first()).toBeVisible();
  await page.goto(`${SETUP}/setup#token=dev-token`);
  await expect(page).toHaveURL(`${SETUP}/setup`);
  await page.getByLabel("User").fill("dev");
  await page.getByLabel("Password", { exact: true }).fill("dev password");
  await page.getByLabel("Repeat password").fill("dev password");
  await page.getByRole("button", { name: "Next" }).click();
  await page.getByRole("button", { name: "Create login" }).click();
  await expect(page.getByRole("heading", { name: "Optional extras" })).toBeVisible();
  await page.getByRole("button", { name: "Skip" }).click();
  await expect(page.getByRole("heading", { name: "Berth is ready" })).toBeVisible();
  await page.getByRole("button", { name: "Open Berth" }).click();
  await expect(h1(page, "Overview")).toBeVisible();
  expect(problems).toEqual([]);
});

test("recovery mode offers a reset", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page, RECOVERY);
  await expect(h1(page, "Berth needs attention")).toBeVisible();
  await page.getByRole("button", { name: "Reset to defaults" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Reset to defaults" }).click();
  await expect(h1(page, "Overview")).toBeVisible();
  expect(problems).toEqual([]);
});

test("an MCP server is added, narrowed, edited and removed", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page.goto(`${NORMAL}/#/mcp`);
  await page.getByRole("button", { name: "Add server" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Id").fill("linear");
  await dialog.getByLabel("URL").fill("http://mcp.example.com/mcp");
  await dialog.getByRole("button", { name: "Add server" }).click();
  await expect(dialog.getByText("Use https unless the host is localhost.")).toBeVisible();
  await dialog.getByLabel("URL").fill("https://mcp.example.com/mcp");
  await dialog.getByLabel("Auth").click();
  await page.getByRole("option", { name: "Bearer token" }).click();
  await dialog.getByLabel("Token").fill("tok-123");
  await dialog.getByRole("button", { name: "Add server" }).click();
  await expect(
    page.getByText(/linear added\. MCP registrations update in the background\./),
  ).toBeVisible();

  const row = page.getByRole("row").filter({ hasText: "mcp.example.com" });
  await expect(row.getByText("HTTP", { exact: true })).toBeVisible();
  await expect(row.getByText("Set", { exact: true })).toBeVisible();
  await row.getByRole("button", { name: "Choose agents for linear" }).click();
  await page.getByRole("checkbox", { name: "Other Agent" }).click();
  await expect(row.getByRole("button", { name: "Choose agents for linear" })).toHaveText(
    "Example Agent",
  );
  await page.keyboard.press("Escape");

  await row.getByRole("button", { name: "Edit linear" }).click();
  await expect(dialog.getByLabel("Id")).toBeDisabled();
  await dialog.getByLabel("URL").fill("https://mcp2.example.com/mcp");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText(/linear saved\./)).toBeVisible();

  const edited = page.getByRole("row").filter({ hasText: "mcp2.example.com" });
  await edited.getByRole("button", { name: "Remove linear" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Remove" }).click();
  await expect(page.getByText(/linear removed\./)).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "mcp2.example.com" })).toHaveCount(0);
  expect(problems).toEqual([]);
});

test("a skill source is added, attached, updated and removed", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page.goto(`${NORMAL}/#/skills`);
  // Toasts are role=dialog too, so dialogs are found by name.
  const add = page.getByRole("dialog", { name: "Add a skill source" });

  // The suggested source opens already found; attach everything at once.
  await page.getByRole("button", { name: "Add anthropics/skills" }).click();
  await expect(add.getByText("10 skills at")).toBeVisible();
  await add.getByRole("checkbox", { name: "Attach all to all agents" }).click();
  await add.getByRole("button", { name: "Add source" }).click();
  await expect(
    page.getByText(/anthropics-skills added; 10 skills attached to all agents\./),
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/skills\/anthropics-skills$/);
  await expect(page.getByRole("heading", { name: "anthropics/skills" })).toBeVisible();

  // Any repository: a bad ref, then the skills with a broken one.
  await page.goto(`${NORMAL}/#/skills`);
  await expect(page.getByRole("button", { name: "Add anthropics/skills" })).toHaveCount(0);
  await page.getByRole("button", { name: "Add source" }).click();
  await add.getByLabel("Repository").fill("acme/eng-skills");
  await add.getByLabel("Ref").fill("missing");
  await add.getByRole("button", { name: "Find skills" }).click();
  await expect(add.getByText("Ref missing not found in that repository")).toBeVisible();
  await add.getByLabel("Ref").fill("main");
  await add.getByRole("button", { name: "Find skills" }).click();
  await expect(add.getByText("2 skills at")).toBeVisible();
  await expect(add.getByText("SKILL.md has no description")).toBeVisible();
  await expect(add.getByLabel("Id")).toHaveValue("acme-eng-skills");
  await add.getByRole("button", { name: "Add source" }).click();
  await expect(page.getByText(/acme-eng-skills added\./)).toBeVisible();
  await expect(page).toHaveURL(/#\/skills\/acme-eng-skills$/);

  // changelog for Example Agent only (Other Agent reads its folder); release-notes for all.
  await page.getByRole("button", { name: "Attach changelog" }).click();
  const picker = page.getByRole("dialog", { name: "Attach changelog" });
  await picker.getByRole("checkbox", { name: /^All agents, including ones added later/ }).click();
  await picker.getByRole("checkbox", { name: /^Other Agent/ }).click();
  await expect(picker.getByText("Through Example Agent")).toBeVisible();
  await picker.getByRole("button", { name: "Attach" }).click();
  await expect(page.getByText(/changelog is attached to Example Agent, Other Agent/)).toBeVisible();
  const changelog = page.getByRole("row").filter({ hasText: "Keep CHANGELOG.md" });
  await expect(changelog.getByText(/^Example Agent, Other Agent/)).toBeVisible();
  await page.getByRole("button", { name: "Attach release-notes" }).click();
  await page
    .getByRole("dialog", { name: "Attach release-notes" })
    .getByRole("button", { name: "Attach" })
    .click();
  await expect(page.getByText(/release-notes is attached to all agents\./)).toBeVisible();
  await page.getByRole("switch", { name: "Turn off release-notes" }).click();
  await expect(page.getByText(/release-notes is off\./)).toBeVisible();

  // Change ref: v2 drops changelog, which is attached.
  await page.getByRole("button", { name: "Change ref" }).click();
  const update = page.getByRole("dialog", { name: "Change ref for acme-eng-skills" });
  await update.getByLabel("Ref").fill("v2");
  await update.getByRole("button", { name: "Preview" }).click();
  await expect(
    update.getByRole("region", { name: "Removed" }).getByText("changelog", { exact: true }),
  ).toBeVisible();
  await expect(update.getByRole("region", { name: "Added" }).getByText("triage")).toBeVisible();
  await expect(update.getByText(/changelog is attached and will be missing/)).toBeVisible();
  await update.getByRole("button", { name: "Update to 89abcdef0123" }).click();
  await expect(
    page.getByText(
      /acme-eng-skills updated to 89abcdef0123\. changelog is no longer in the source/,
    ),
  ).toBeVisible();

  // The missing skill keeps its row until it is detached.
  await expect(page.getByText("Missing from 89abcdef0123")).toBeVisible();
  await page.getByRole("button", { name: "Detach changelog" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Detach" }).click();
  await expect(page.getByText(/changelog detached\./)).toBeVisible();
  await expect(page.getByRole("button", { name: "Detach changelog" })).toHaveCount(0);

  // Update with nothing new.
  await page.getByRole("button", { name: "Update", exact: true }).click();
  const again = page.getByRole("dialog", { name: "Update acme-eng-skills" });
  await expect(again.getByText(/Already up to date/)).toBeVisible();
  // The footer's Close, not the dialog's own close button.
  await again.getByRole("button", { name: "Close" }).and(again.getByText("Close")).click();

  // All skills lists attachments across sources and links to them.
  await page.goto(`${NORMAL}/#/skills?view=flat`);
  await expect(
    page
      .getByRole("row")
      .filter({ hasText: "release-notes" })
      .getByRole("link", { name: "acme-eng-skills" }),
  ).toBeVisible();
  await expect(
    page
      .getByRole("row")
      .filter({ hasText: "xlsx" })
      .getByRole("link", { name: "anthropics-skills" }),
  ).toBeVisible();

  // Remove both; with skills attached, the id is typed first.
  for (const id of ["acme-eng-skills", "anthropics-skills"]) {
    await page.goto(`${NORMAL}/#/skills/${id}`);
    await page.getByRole("button", { name: "Remove", exact: true }).click();
    const remove = page.getByRole("dialog", { name: `Remove ${id}?` });
    await expect(remove.getByRole("button", { name: "Remove source" })).toBeDisabled();
    await remove.getByLabel(`Type ${id} to confirm`).fill(id);
    await remove.getByRole("button", { name: "Remove source" }).click();
    await expect(page.getByText(new RegExp(`${id} removed\\.`))).toBeVisible();
    await expect(page).toHaveURL(/#\/skills$/);
  }
  await expect(page.getByRole("button", { name: "Add anthropics/skills" })).toBeVisible();
  expect(problems).toEqual([]);
});

test("agents sign in with a pasted code and a device code, and sign out", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page.goto(`${NORMAL}/#/agents`);
  const example = page.getByRole("row").filter({ hasText: "@example/agent-cli" });
  await expect(example.getByText("Signed out")).toBeVisible();

  await example.getByRole("button", { name: "Sign in Example Agent with Example account" }).click();
  // Toasts are dialogs too; name the sign-in one.
  const dialog = page.getByRole("dialog", { name: /^Sign in to / });
  await expect(dialog.getByText("Sign in to Example Agent · Example account")).toBeVisible();
  await expect(dialog.getByRole("link", { name: "Open example.com/authorize" })).toBeVisible();
  await dialog.getByLabel("Code").fill("wrong");
  await dialog.getByRole("button", { name: "Submit code" }).click();
  await expect(
    dialog.getByText(/That code didn't work\. Open the sign-in page again/),
  ).toBeVisible();
  await dialog.getByLabel("Code").fill("  good-code ");
  await dialog.getByRole("button", { name: "Submit code" }).click();
  await expect(page.getByText("Example Agent: signed in as you@example.org.")).toBeVisible();
  await expect(example.getByText("you@example.org")).toBeVisible();

  const other = page.getByRole("row").filter({ hasText: "@example/other-cli" });
  await other.getByRole("button", { name: "Sign in Other Agent with GitHub Copilot" }).click();
  await expect(dialog.getByText("WXYZ-1234")).toBeVisible();
  await expect(page.getByText("Other Agent: signed in as you@example.org.")).toBeVisible();

  await other.getByRole("button", { name: "Sign in Other Agent with ChatGPT" }).click();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toHaveCount(0);

  await example.getByRole("button", { name: "Sign Example Agent out of Example account" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByText("Example Agent is signed out of Example account.")).toBeVisible();
  await expect(example.getByText("Signed out")).toBeVisible();
  expect(problems).toEqual([]);
});

test("code hosts sign in with a device code and a token, and sign out", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page.goto(`${NORMAL}/#/accounts`);
  await expect(h1(page, "Accounts")).toBeVisible();

  await page.getByRole("button", { name: "Sign in to GitHub" }).click();
  await expect(page.getByText("ABCD-1234")).toBeVisible();
  await expect(page.getByText("GitHub: signed in.")).toBeVisible();
  await page.getByRole("button", { name: "Sign out of GitHub" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByText("Signed out of GitHub.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Sign in to GitHub" })).toBeVisible();

  await page.getByRole("textbox", { name: "Server", exact: true }).fill("https://codeberg.org");
  await expect(page.getByRole("link", { name: "Create a token on codeberg.org" })).toHaveAttribute(
    "href",
    "https://codeberg.org/user/settings/applications",
  );
  await page.getByLabel("Access token").fill("bad");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByText("invalid username, password or token")).toBeVisible();
  await page.getByLabel("Access token").fill("good");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByText("Gitea: signed in to codeberg.org.")).toBeVisible();
  await expect(page.getByLabel("Access token")).toHaveValue("");
  await expect(page.getByRole("heading", { name: "Add a server" })).toBeVisible();

  await page.getByRole("button", { name: "Sign out of codeberg.org" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByText("Signed out of codeberg.org.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Sign out of codeberg.org" })).toHaveCount(0);
  expect(problems).toEqual([]);
});

test("a registry agent is added, gets a Sign-in column and is removed", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page.goto(`${NORMAL}/#/agents`);
  await expect(h1(page, "Agents")).toBeVisible();
  await page.getByRole("button", { name: "Add agent" }).click();
  // Base UI toasts are dialogs too: scope by name.
  const dialog = page.getByRole("dialog", { name: "Add an agent" });
  await expect(dialog.getByText("GitHub Copilot CLI")).toBeVisible();
  await dialog.getByRole("button", { name: "Add GitHub Copilot CLI" }).click();
  await expect(page.getByText("GitHub Copilot CLI added. Sign in from its row.")).toBeVisible();
  await expect(dialog).toBeHidden();
  const row = page
    .getByRole("table", { name: "Agents" })
    .getByRole("row")
    .filter({ hasText: "GitHub Copilot CLI" });
  await expect(row.getByText("Added")).toBeVisible();
  await expect(
    row.getByRole("button", { name: "Sign in GitHub Copilot CLI with GitHub" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Add agent" })).toBeHidden();
  await row.getByRole("button", { name: "Remove GitHub Copilot CLI" }).click();
  const confirm = page.getByRole("alertdialog", { name: "Remove GitHub Copilot CLI?" });
  await expect(confirm.getByText("~/.copilot")).toBeVisible();
  await confirm.getByRole("button", { name: "Remove" }).click();
  await expect(row).toBeHidden();
  await expect(page.getByRole("button", { name: "Add agent" })).toBeVisible();
  expect(problems).toEqual([]);
});

test("a workspace is cloned, launched and forgotten", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page.goto(`${NORMAL}/#/workspaces`);
  await expect(page.getByText(/No workspaces yet\./)).toBeVisible();

  // Toasts are role=dialog too, so dialogs are found by name.
  const add = page.getByRole("dialog", { name: "Add a workspace" });
  await page.getByRole("button", { name: "Add workspace" }).first().click();
  await add.getByLabel("Repository").fill("acme/private");
  await add.getByRole("button", { name: "Clone", exact: true }).click();
  await expect(add.getByText(/Repository not found, or it is private\./)).toBeVisible();
  await add.getByLabel("Repository").fill("acme/api");
  await expect(add.getByLabel("Folder")).toHaveValue("github.com/acme/api");
  await expect(add.getByLabel("Id")).toHaveValue("acme-api");
  await add.getByRole("button", { name: "Clone", exact: true }).click();

  // The new workspace's page opens on its Overview.
  await expect(h1(page, "acme-api")).toBeVisible();
  await expect(page.getByText("main · 2 changes")).toBeVisible();
  await expect(page.getByText("ws-acme-api")).toBeVisible();

  // Launching an agent opens the Terminal tab on the workspace's session.
  await page.getByRole("button", { name: "Launch in acme-api" }).click();
  await page.getByRole("menuitem", { name: "Example Agent" }).click();
  await expect(page).toHaveURL(/tab=terminal/);
  await expect(page.getByRole("list", { name: "Windows" }).getByText("example")).toBeVisible();
  await expect(
    page
      .frameLocator('iframe[title="Terminal for acme-api"]')
      .getByText("The terminal is not running"),
  ).toBeVisible();

  // The list shows the session; Forget keeps the folder and empties the list.
  await page.getByRole("link", { name: "Workspaces", exact: true }).click();
  const row = page.getByRole("row").filter({ hasText: "acme-api" });
  await expect(row.getByText("main · 2 changes")).toBeVisible();
  await expect(row.getByText("Running")).toBeVisible();
  await row.getByRole("button", { name: "Manage" }).click();
  await page.getByRole("button", { name: "Forget", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Forget" }).click();
  await expect(page.getByText(/acme-api forgotten; its folder stays\./)).toBeVisible();
  await expect(h1(page, "Workspaces")).toBeVisible();
  await expect(page.getByText(/No workspaces yet\./)).toBeVisible();
  expect(problems).toEqual([]);
});

test("skills and MCP servers are attached to a workspace", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);

  // A cloned workspace (its .mcp.json is tracked by git in the fake).
  await page.goto(`${NORMAL}/#/workspaces`);
  await page.getByRole("button", { name: "Add workspace" }).first().click();
  const addWs = page.getByRole("dialog", { name: "Add a workspace" });
  await addWs.getByLabel("Repository").fill("acme/api");
  await addWs.getByRole("button", { name: "Clone", exact: true }).click();
  await expect(h1(page, "acme-api")).toBeVisible();

  // A source; changelog goes to all agents and, linked, to the workspace.
  await page.goto(`${NORMAL}/#/skills`);
  await page.getByRole("button", { name: "Add source" }).click();
  const addSrc = page.getByRole("dialog", { name: "Add a skill source" });
  await addSrc.getByLabel("Repository").fill("acme/eng-skills");
  await addSrc.getByRole("button", { name: "Find skills" }).click();
  await addSrc.getByRole("button", { name: "Add source" }).click();
  await expect(page).toHaveURL(/#\/skills\/acme-eng-skills$/);
  await page.getByRole("button", { name: "Attach changelog" }).click();
  const picker = page.getByRole("dialog", { name: "Attach changelog" });
  await picker.getByRole("checkbox", { name: /^acme-api/ }).click();
  await expect(picker.getByRole("group", { name: "How acme-api gets changelog" })).toBeVisible();
  await picker.getByRole("button", { name: "Attach" }).click();
  await expect(page.getByText(/changelog is attached to all agents and acme-api\./)).toBeVisible();
  await expect(
    page
      .getByRole("row")
      .filter({ hasText: "Keep CHANGELOG.md" })
      .getByText("All agents · acme-api"),
  ).toBeVisible();

  // The workspace's Skills tab; release-notes, vendored, has local edits in the fake.
  await page.goto(`${NORMAL}/#/workspaces/acme-api?tab=skills`);
  const changelog = page.getByRole("row").filter({ hasText: "changelog" });
  await expect(changelog.getByText("Linked")).toBeVisible();
  const attachVendored = async () => {
    await page.getByRole("button", { name: "Attach skill…" }).click();
    const dialog = page.getByRole("dialog", { name: "Attach a skill to acme-api" });
    await dialog.getByLabel("Search skills").fill("release");
    await dialog.getByRole("button", { name: "Vendor" }).click();
    await dialog.getByRole("button", { name: "Attach release-notes from acme-eng-skills" }).click();
    // The first attach's toast may still show: look at the newest.
    await expect(page.getByText(/release-notes is vendored into acme-api\./).last()).toBeVisible();
  };
  await attachVendored();
  const notes = page.getByRole("row").filter({ hasText: "release-notes" });
  await expect(notes.getByText("Local edits")).toBeVisible();

  // Stop managing: Berth lets go and the row goes.
  await notes.getByRole("button", { name: "Stop managing release-notes" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Stop managing" }).click();
  await expect(page.getByText(/release-notes in acme-api is yours now/)).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "release-notes" })).toHaveCount(0);

  // Again, then Overwrite: the copy becomes Berth's again.
  await attachVendored();
  await expect(notes.getByText("Local edits")).toBeVisible();
  await notes.getByRole("button", { name: "Overwrite release-notes" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Overwrite" }).click();
  await expect(page.getByText(/Berth replaces release-notes in acme-api/)).toBeVisible();
  await expect(notes.getByText("Vendored")).toBeVisible();

  // The MCP tab: Files MCP in this workspace; a tracked file and trust notes.
  await page.getByRole("button", { name: "MCP", exact: true }).click();
  await page.getByRole("switch", { name: "Add Files MCP to acme-api" }).click();
  await expect(page.getByText(/Files MCP is attached to acme-api\./)).toBeVisible();
  const files = page.getByRole("list", { name: "Project files" });
  await expect(files.getByText(".mcp.json")).toBeVisible();
  await expect(files.getByText(/Tracked by git: Berth's entries show in git diff\./)).toBeVisible();
  await expect(
    page.getByText("Other Agent trusts this folder while a server is attached here."),
  ).toBeVisible();
  await page.goto(`${NORMAL}/#/mcp`);
  await expect(page.getByRole("button", { name: "Choose agents for Files MCP" })).toHaveText(
    "All agents · acme-api",
  );

  // Forget drops every reference; then remove the source.
  await page.goto(`${NORMAL}/#/workspaces/acme-api`);
  await page.getByRole("button", { name: "Forget", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Forget" }).click();
  await expect(page.getByText(/acme-api forgotten; its folder stays\./)).toBeVisible();
  await page.goto(`${NORMAL}/#/mcp`);
  await expect(page.getByRole("button", { name: "Choose agents for Files MCP" })).toHaveText(
    "All agents",
  );
  await page.goto(`${NORMAL}/#/skills/acme-eng-skills`);
  await expect(
    page
      .getByRole("row")
      .filter({ hasText: "Keep CHANGELOG.md" })
      .getByText("All agents", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Remove", exact: true }).click();
  const remove = page.getByRole("dialog", { name: "Remove acme-eng-skills?" });
  await remove.getByLabel("Type acme-eng-skills to confirm").fill("acme-eng-skills");
  await remove.getByRole("button", { name: "Remove source" }).click();
  await expect(page).toHaveURL(/#\/skills$/);
  expect(problems).toEqual([]);
});

test("a workspace's instructions are edited and shared, and its env is set", async ({ page }) => {
  const problems = await watch(page);
  await signIn(page);
  await page.goto(`${NORMAL}/#/workspaces`);
  await page.getByRole("button", { name: "Add workspace" }).first().click();
  const add = page.getByRole("dialog", { name: "Add a workspace" });
  await add.getByLabel("Repository").fill("acme/api");
  await add.getByRole("button", { name: "Clone", exact: true }).click();
  await expect(h1(page, "acme-api")).toBeVisible();

  // CLAUDE.md is written, then shared: it has its own content, so it is merged.
  await page.goto(`${NORMAL}/#/workspaces/acme-api?tab=instructions`);
  await page.getByLabel("CLAUDE.md", { exact: true }).fill("# Claude rules\n");
  await page.getByRole("button", { name: "Save CLAUDE.md" }).click();
  await expect(page.getByText("CLAUDE.md saved.")).toBeVisible();
  await page.getByRole("switch", { name: "Agents in acme-api share AGENTS.md" }).click();
  await expect(page.getByText(/CLAUDE.md has its own content/)).toBeVisible();
  await page.getByRole("button", { name: "Append to AGENTS.md and replace" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Append and replace" }).click();
  await expect(page.getByText("CLAUDE.md now shares AGENTS.md.")).toBeVisible();
  await expect(page.getByText("Shares AGENTS.md", { exact: true })).toBeVisible();
  await expect(page.getByLabel("AGENTS.md", { exact: true })).toHaveValue("# Claude rules\n");

  // A save over a file changed on disk keeps the draft.
  await page.getByLabel("AGENTS.md", { exact: true }).fill("<<drift>>");
  await page.getByRole("button", { name: "Save AGENTS.md" }).click();
  await expect(page.getByText(/changed on disk since you opened it/)).toBeVisible();
  await expect(page.getByLabel("AGENTS.md", { exact: true })).toHaveValue("<<drift>>");
  await page.getByRole("button", { name: "Discard my edits" }).click();
  await expect(page.getByLabel("AGENTS.md", { exact: true })).toHaveValue("# Claude rules\n");

  // Env: a plain variable and a secret; the secret is replaced, cleared and removed.
  await page.goto(`${NORMAL}/#/workspaces/acme-api?tab=env`);
  await page.getByRole("button", { name: "Add variable" }).click();
  let dialog = page.getByRole("dialog", { name: "Add a variable to acme-api" });
  await dialog.getByLabel("Name").fill("log_level");
  await dialog.getByLabel("Value").fill("debug");
  await dialog.getByRole("button", { name: "Add variable" }).click();
  await expect(
    page.getByRole("row").filter({ hasText: "LOG_LEVEL" }).getByText("debug"),
  ).toBeVisible();
  await page.getByRole("button", { name: "Add variable" }).click();
  dialog = page.getByRole("dialog", { name: "Add a variable to acme-api" });
  await dialog.getByLabel("Name").fill("STRIPE_KEY");
  await dialog.getByRole("checkbox", { name: "Secret" }).click();
  await dialog.getByLabel("Value").fill("sk_test_123");
  await dialog.getByRole("button", { name: "Add variable" }).click();
  const secret = page.getByRole("row").filter({ hasText: "STRIPE_KEY" });
  await expect(secret.getByText("Set", { exact: true })).toBeVisible();
  await expect(page.getByText("sk_test_123")).toHaveCount(0);
  await secret.getByRole("button", { name: "Clear STRIPE_KEY" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Clear" }).click();
  await expect(secret.getByText(/Not set/)).toBeVisible();
  await secret.getByRole("button", { name: "Remove STRIPE_KEY" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Remove" }).click();
  await expect(page.getByRole("row").filter({ hasText: "STRIPE_KEY" })).toHaveCount(0);

  await page.getByRole("button", { name: "Restart session" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Restart session" }).click();
  await expect(page.getByText("ws-acme-api restarted.")).toBeVisible();
  expect(problems).toEqual([]);
});
