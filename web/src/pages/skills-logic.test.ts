import { describe, expect, it } from "vitest";
import type { AgentPackage, Config, Job, ScanRow, Skill } from "@/api/types";
import { CATALOG } from "./skills-catalog";
import {
  agentsPatch,
  defaultPicks,
  groupOf,
  repoLabel,
  rowNote,
  scanOf,
  selectable,
  skillGroups,
  updateText,
  normalizeRepo,
  pathError,
  reachLabel,
  refError,
  removePatch,
  repoError,
  skillAgents,
  skillRows,
  sourceLabel,
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
const skill: Skill = {
  repo: "https://github.com/anthropics/skills",
  path: "skills/pdf",
  ref: "main",
  commit: "0123456789abcdef0123456789abcdef01234567",
  description: "PDF tools",
  agents: null,
  enabled: true,
};

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
  it("lists skills by id with a short source", () => {
    const cfg = { skills: { pdf: skill, apdf: { ...skill, path: "" } } } as unknown as Config;
    expect(skillRows(cfg).map((r) => r.id)).toEqual(["apdf", "pdf"]);
    expect(sourceLabel(skill)).toBe("anthropics/skills · skills/pdf");
    expect(sourceLabel({ ...skill, repo: "https://git.example.org/x/y.git", path: "" })).toBe(
      "git.example.org/x/y",
    );
    expect(skillRows({} as Config)).toEqual([]);
  });
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
  it("builds patches", () => {
    expect(agentsPatch("pdf", null)).toEqual({ skills: { pdf: { agents: null } } });
    expect(removePatch("pdf")).toEqual({ skills: { pdf: null } });
  });
});

describe("catalog", () => {
  it("has unique ids, valid sources and short descriptions", () => {
    const ids = CATALOG.map((c) => c.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const c of CATALOG) {
      expect(repoError(c.repo)).toBe("");
      expect(pathError(c.path)).toBe("");
      expect(c.path.endsWith(`/${c.id}`)).toBe(true);
      expect(c.description.length).toBeLessThanOrEqual(80);
    }
  });
});

const job = (result: Job["result"]): Job => ({
  id: 1,
  verb: "skills.update",
  group: "skills",
  args: [],
  state: "succeeded",
  started_at: "",
  finished_at: "",
  exit_code: 0,
  log_truncated: false,
  result,
});

describe("groups", () => {
  const acme = "https://github.com/acme/eng-skills";
  const cfg = {
    skills: {
      pdf: skill,
      docx: { ...skill, path: "skills/docx" },
      notes: { ...skill, repo: acme, path: "skills/notes", ref: "v2" },
      changelog: { ...skill, repo: acme, path: "skills/changelog" },
      alpha: { ...skill, repo: acme, path: "skills/alpha" },
    },
  } as unknown as Config;

  it("groups by repo and ref, sorted by repo, ref, then name", () => {
    const groups = skillGroups(cfg);
    expect(
      groups.map((g) => `${repoLabel(g.repo)}@${g.ref}:${g.rows.map((r) => r.id).join(",")}`),
    ).toEqual([
      "acme/eng-skills@main:alpha,changelog",
      "acme/eng-skills@v2:notes",
      "anthropics/skills@main:docx,pdf",
    ]);
    expect(skillGroups({} as Config)).toEqual([]);
  });

  it("finds a skill's group", () => {
    const groups = skillGroups(cfg);
    expect(groupOf(groups, "pdf").map((r) => r.id)).toEqual(["docx", "pdf"]);
    expect(groupOf(groups, "notes").map((r) => r.id)).toEqual(["notes"]);
    expect(groupOf(groups, "gone")).toEqual([]);
  });

  it("labels repositories", () => {
    expect(repoLabel("https://github.com/anthropics/skills")).toBe("anthropics/skills");
    expect(repoLabel("https://git.example.org/x/y.git")).toBe("git.example.org/x/y");
  });
});

describe("scan checklist", () => {
  const rows: ScanRow[] = [
    { path: "skills/a", name: "a", description: "A" },
    { path: "skills/b", name: "b", description: "B", installed: true },
    { path: "skills/c", error: "SKILL.md has no description" },
    { path: "", name: "root", description: "R" },
  ];

  it("picks every row that can be added", () => {
    expect(rows.map(selectable)).toEqual([true, false, false, true]);
    expect(defaultPicks(rows)).toEqual(["skills/a", ""]);
    expect(defaultPicks([rows[1], rows[2]])).toEqual([]);
  });

  it("says why a row cannot be picked", () => {
    expect(rows.map(rowNote)).toEqual(["", "Already installed", "SKILL.md has no description", ""]);
  });

  it("reads a scan job", () => {
    expect(scanOf(job({ commit: "abc", skills: rows }))).toEqual({ commit: "abc", skills: rows });
    expect(scanOf(job({ changed: true }))).toBeNull();
    expect(scanOf(job(null))).toBeNull();
  });
});

describe("update toasts", () => {
  it("names one skill and counts several", () => {
    expect(updateText(job({ changed: true, skills: ["pdf"] }), " BG")).toBe("pdf updated. BG");
    expect(updateText(job({ changed: true, skills: ["a", "b", "c"] }), " BG")).toBe(
      "3 skills updated. BG",
    );
    expect(updateText(job({ changed: false, skills: [] }), " BG")).toBe("Already up to date.");
  });
});
