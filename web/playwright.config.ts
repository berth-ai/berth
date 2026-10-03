import { defineConfig, devices } from "@playwright/test";

// Three dev servers, one per broker mode, each serving the built web/dist
// through the real Go panel and its CSP.
const server = (port: number, mode: string) => ({
  command: `go run ../internal/panel/devserver -addr 127.0.0.1:${port} -dist dist -mode ${mode}`,
  url: `http://127.0.0.1:${port}/login`,
  reuseExistingServer: false,
  timeout: 180_000,
});

export default defineConfig({
  testDir: "e2e",
  workers: 1,
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: { baseURL: "http://127.0.0.1:7691", trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [server(7691, "normal"), server(7692, "setup"), server(7693, "recovery")],
});
