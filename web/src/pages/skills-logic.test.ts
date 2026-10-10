import { describe, expect, it } from "vitest";
import type { AgentPackage } from "@/api/types";
import {
  normalizeRepo,
  parseSkillView,
  pathError,
  reachLabel,
  refError,
  repoError,
  repoLabel,
  skillAgents,
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
