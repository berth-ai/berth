// @vitest-environment node
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";

const root = new URL("..", import.meta.url).pathname;
const hex = /#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3})?(?:[0-9a-fA-F]{2})?\b/;

function files(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) files(path, out);
    else if (
      /\.(tsx?|css)$/.test(name) &&
      !/\.test\.tsx?$/.test(name) &&
      name !== "routeTree.gen.ts"
    )
      out.push(path);
  }
  return out;
}

const sources = [
  ...files(join(root, "src")),
  ...["index.html", "login.html", "setup.html", "unavailable.html"].map((p) => join(root, p)),
];

describe("source rules", () => {
  it("keeps hex colours inside the tokens block", () => {
    for (const path of sources) {
      let text = readFileSync(path, "utf8");
      if (path.endsWith("styles/tokens.css")) {
        text =
          text.slice(0, text.indexOf("/* tokens:start */")) +
          text.slice(text.indexOf("/* tokens:end */"));
      }
      expect(text.match(hex)?.[0], relative(root, path)).toBeUndefined();
    }
  });

  it("never builds markup from strings or injects style elements", () => {
    for (const path of sources) {
      const text = readFileSync(path, "utf8");
      for (const bad of [
        "innerHTML",
        "outerHTML",
        "insertAdjacentHTML",
        "document.write",
        "eval(",
        "dangerouslySetInnerHTML",
        "<style",
      ]) {
        expect(text.includes(bad), `${relative(root, path)} uses ${bad}`).toBe(false);
      }
    }
  });

  it("takes the setup token from the fragment before rendering, never from a query", () => {
    const main = readFileSync(join(root, "src/auth/setup-main.tsx"), "utf8");
    const take = main.indexOf("takeSetupToken()");
    expect(take).toBeGreaterThan(-1);
    expect(take).toBeLessThan(main.indexOf("createRoot("));
    for (const path of sources)
      expect(readFileSync(path, "utf8").includes("?token"), relative(root, path)).toBe(false);
  });
});
