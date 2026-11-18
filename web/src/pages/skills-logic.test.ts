import { describe, expect, it } from "vitest";
import type { AgentPackage } from "@/api/types";
import {
  commitUrl,
  normalizeSite,
  shortCommit,
  siteError,
  sourceTitle,
  normalizeRepo,
  parseSkillView,
  pathError,
  reachLabel,
  refError,
  repoError,
  repoHome,
  repoLabel,
  skillAgents,
  treeUrl,
  unsupportedAgents,
  viaNotes,
} from "./skills-logic";

const agent = (pkg: string, name: string, extra: Partial<AgentPackage> = {}): AgentPackage => ({
  package: pkg,
  name,
  kind: "agent",
  version: "1",
  enabled: true,
  bins: [],
  keys: [],
  ...extra,
});
const claude = agent("@anthropic-ai/claude-code", "Claude Code", {
  skills_dir: "~/.claude/skills",
});
const opencode = agent("opencode-ai", "OpenCode", {
  skills_dir: "$XDG_CONFIG_HOME/opencode/skills",
  skills_via: ["@anthropic-ai/claude-code"],
});
const plain = agent("other", "Other");

describe("repo input", () => {
  it("normalizes shorthands", () => {
    expect(normalizeRepo("anthropics/skills")).toBe("https://github.com/anthropics/skills");
    expect(normalizeRepo(" github.com/anthropics/skills ")).toBe(
      "https://github.com/anthropics/skills",
    );
    expect(normalizeRepo("https://gitlab.com/a/b.git")).toBe("https://gitlab.com/a/b.git");
  });
  it("mirrors the broker's rules", () => {
    expect(repoError("")).toMatch(/Enter/);
    expect(repoError("http://github.com/a/b")).toMatch(/https/);
    expect(repoError("https://me:pw@github.com/a/b")).toMatch(/Accounts/);
    expect(repoError("https://github.com/a/b?x")).toMatch(/\?/);
    expect(repoError("anthropics/skills")).toBe("");
  });
  it("checks path and ref", () => {
    expect(pathError("")).toBe("");
    expect(pathError("skills/pdf")).toBe("");
    expect(pathError("../x")).not.toBe("");
    expect(pathError("a//b")).not.toBe("");
    expect(refError("main")).toBe("");
    expect(refError("-x")).not.toBe("");
    expect(refError("a..b")).not.toBe("");
    expect(refError("")).not.toBe("");
  });
});

describe("rows and labels", () => {
  it("splits agents by support", () => {
    expect(skillAgents([claude, opencode, plain]).map((a) => a.name)).toEqual([
      "Claude Code",
      "OpenCode",
    ]);
    expect(unsupportedAgents([claude, plain]).map((a) => a.name)).toEqual(["Other"]);
  });
  it("shows reach through another agent's directory", () => {
    const agents = [claude, opencode];
    expect(viaNotes(null, agents)).toEqual({});
    expect(viaNotes(["@anthropic-ai/claude-code"], agents)).toEqual({
      "opencode-ai": "Through Claude Code",
    });
    expect(viaNotes(["opencode-ai"], agents)).toEqual({});
    expect(reachLabel(["@anthropic-ai/claude-code"], agents)).toBe("Claude Code, OpenCode");
    expect(reachLabel(null, agents)).toBe("All agents");
    expect(reachLabel(["gone"], agents)).toBe("No installed agent");
  });
  it("labels repositories", () => {
    expect(repoLabel("https://github.com/anthropics/skills")).toBe("anthropics/skills");
    expect(repoLabel("https://git.example.org/x/y.git")).toBe("git.example.org/x/y");
  });
});

describe("skill views", () => {
  it("reads the view from the URL, defaulting to by source", () => {
    expect(parseSkillView("flat")).toBe("flat");
    expect(parseSkillView("source")).toBe("source");
    expect(parseSkillView("nope")).toBe("source");
    expect(parseSkillView(undefined)).toBe("source");
  });
});

describe("repository links", () => {
  const C = "0123456789abcdef0123456789abcdef01234567";
  it("links GitHub, GitLab and Forgejo folders and commits", () => {
    expect(repoHome("https://github.com/cursor/plugins/")).toBe(
      "https://github.com/cursor/plugins",
    );
    expect(repoHome("https://example.com/o/r.git")).toBe("https://example.com/o/r");
    expect(treeUrl("https://github.com/o/r", C, "skills/pdf")).toBe(
      `https://github.com/o/r/tree/${C}/skills/pdf`,
    );
    expect(treeUrl("https://github.com/o/r.git", C, "")).toBe(`https://github.com/o/r/tree/${C}`);
    expect(treeUrl("https://gitlab.com/g/r", C, "a b")).toBe(
      `https://gitlab.com/g/r/-/tree/${C}/a%20b`,
    );
    expect(treeUrl("https://codeberg.org/o/r", C, "x")).toBe(
      `https://codeberg.org/o/r/src/commit/${C}/x`,
    );
    expect(commitUrl("https://github.com/o/r", C)).toBe(`https://github.com/o/r/commit/${C}`);
    expect(commitUrl("https://gitlab.example.com/o/r", C)).toBe(
      `https://gitlab.example.com/o/r/-/commit/${C}`,
    );
  });
  it("gives no deep link on an unknown host", () => {
    expect(treeUrl("https://git.example.com/o/r", C, "x")).toBeNull();
    expect(commitUrl("https://git.example.com/o/r", C)).toBeNull();
    expect(commitUrl("not a url", C)).toBeNull();
  });
});

describe("websites", () => {
  it("names a website by its host and shortens its digest", () => {
    const repo = "https://docs.example.org/.well-known/agent-skills/index.json";
    expect(sourceTitle({ kind: "well-known", repo })).toBe("docs.example.org");
    expect(sourceTitle({ repo: "https://github.com/acme/skills" })).toBe("acme/skills");
    expect(shortCommit("sha256:" + "ab".repeat(32))).toBe("abababababab");
    expect(shortCommit("0123456789abcdef0123")).toBe("0123456789ab");
  });

  it("checks a website URL", () => {
    expect(siteError("")).toMatch(/Enter the website/);
    expect(siteError("http://example.com")).toBe("Use an https:// URL.");
    expect(siteError("https://u:p@example.com")).toMatch(/user name/);
    expect(siteError("example.com")).toBe("");
    expect(normalizeSite("example.com/docs")).toBe("https://example.com/docs");
  });
});
