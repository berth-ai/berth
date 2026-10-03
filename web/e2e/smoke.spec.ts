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
    ["#/agents", "Agents"],
    ["#/mcp", "MCP servers"],
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
    ["#/agents", "Agents"],
    ["#/mcp", "MCP servers"],
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
