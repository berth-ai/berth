import { describe, expect, it } from "vitest";
import type { AgentPackage, Job, Source, SourceDiff } from "@/api/types";
import {
  addedText,
  agentsOf,
  attachedRows,
  attachPatch,
  conflicts,
  detachPatch,
  diffOf,
  dropsAttached,
  enabledPatch,
  foundRows,
  hasSuggested,
  missingNames,
  newScanOf,
  pickAgent,
  sourceIdError,
  sourceLabel,
  sourceList,
  sourceSummary,
  updatedText,
  whoText,
} from "./sources-logic";

const C1 = "0123456789abcdef0123456789abcdef01234567";
const agent = (pkg: string, name: string, via?: string[]): AgentPackage => ({
  package: pkg,
  name,
  kind: "agent",
  version: "1",
  enabled: true,
  bins: [],
  keys: [],
  skills_dir: `~/.${name.toLowerCase()}/skills`,
  skills_via: via,
});
const AGENTS = [agent("a", "Alpha"), agent("b", "Beta", ["a"]), agent("c", "Gamma")];

const src = (over: Partial<Source> = {}): Source => ({
  repo: "https://github.com/acme/eng-skills",
  ref: "main",
  path: "skills",
  commit: C1,
  found: {
    changelog: { path: "skills/changelog", description: "Changelog" },
    broken: { path: "skills/broken", error: "SKILL.md has no description" },
    notes: { path: "skills/notes", description: "Notes" },
  },
  attached: { notes: { enabled: true, agents: null, workspaces: {} } },
  ...over,
});

const job = (result: unknown): Job => ({
  id: 1,
  verb: "skills.sources.preview",
  group: "skills",
  args: [],
  state: "succeeded",
  started_at: "",
  finished_at: "",
  exit_code: 0,
  log_truncated: false,
  result: result as Job["result"],
});

describe("sources", () => {
  it("lists sources by id and spots the suggested one", () => {
    const sources = { zed: src(), acme: src({ repo: "https://github.com/anthropics/skills" }) };
    expect(sourceList(sources).map((e) => e.id)).toEqual(["acme", "zed"]);
    expect(sourceList(null)).toEqual([]);
    expect(hasSuggested(sources)).toBe(true);
    expect(hasSuggested({ zed: src() })).toBe(false);
  });

  it("labels and summarizes a source", () => {
    expect(sourceLabel(src())).toBe("acme/eng-skills · skills");
    expect(sourceLabel(src({ path: "" }))).toBe("acme/eng-skills");
    expect(sourceSummary(src())).toBe("2 skills · 1 attached");
  });

  it("lists found rows with their attachments and the missing names", () => {
    const s = src({
      attached: { notes: { enabled: true }, gone: { enabled: true }, broken: { enabled: false } },
    });
    expect(foundRows(s).map((r) => [r.name, r.attachment !== null])).toEqual([
      ["broken", true],
      ["changelog", false],
      ["notes", true],
    ]);
    expect(missingNames(s)).toEqual(["broken", "gone"]);
  });

  it("lists attached skills across sources", () => {
    const rows = attachedRows({
      x: src({ attached: { notes: { enabled: true }, gone: { enabled: false } } }),
      y: src({ attached: { changelog: { enabled: true, agents: ["a"] } } }),
    });
    expect(rows.map((r) => [r.name, r.id, r.missing])).toEqual([
      ["changelog", "y", false],
      ["gone", "x", true],
      ["notes", "x", false],
    ]);
  });
});

describe("attaching", () => {
  it("reads a missing agents key as every agent", () => {
    expect(agentsOf({ enabled: true })).toBeNull();
    expect(agentsOf({ enabled: true, agents: [] })).toEqual([]);
  });

  it("finds the agents another source already gives a skill", () => {
    const sources = {
      x: src(),
      y: src({ attached: { notes: { enabled: false, agents: ["c"] } } }),
    };
    expect(conflicts(sources, "y", "notes", AGENTS)).toEqual({ "*": "x", a: "x", b: "x", c: "x" });
    expect(conflicts(sources, "x", "notes", AGENTS)).toEqual({ c: "y" });
    expect(conflicts(sources, "x", "changelog", AGENTS)).toEqual({});
  });

  it("ticks one agent without turning a full list into every agent", () => {
    expect(pickAgent(null, ["a", "b"], "b", false)).toEqual(["a"]);
    expect(pickAgent(["a"], ["a", "b"], "b", true)).toEqual(["a", "b"]);
    expect(pickAgent(["a"], ["a", "b"], "a", false)).toEqual([]);
  });

  it("builds attach, detach and enable patches", () => {
    expect(attachPatch("x", "notes", null, null)).toEqual({
      skills: {
        sources: { x: { attached: { notes: { enabled: true, agents: null, workspaces: {} } } } },
      },
    });
    expect(attachPatch("x", "notes", ["a"], { enabled: true })).toEqual({
      skills: { sources: { x: { attached: { notes: { agents: ["a"] } } } } },
    });
    expect(attachPatch("x", "notes", [], { enabled: true })).toEqual(detachPatch("x", "notes"));
    expect(attachPatch("x", "notes", [], { enabled: true, workspaces: { w: "link" } })).toEqual({
      skills: { sources: { x: { attached: { notes: { agents: [] } } } } },
    });
    expect(detachPatch("x", "notes")).toEqual({
      skills: { sources: { x: { attached: { notes: null } } } },
    });
    expect(enabledPatch("x", "notes", false)).toEqual({
      skills: { sources: { x: { attached: { notes: { enabled: false } } } } },
    });
  });

  it("names who gets a skill", () => {
    expect(whoText({ enabled: true }, AGENTS)).toBe("All agents");
    expect(whoText({ enabled: true, agents: [] }, AGENTS)).toBe("No agent");
    expect(whoText({ enabled: true, agents: ["a"] }, AGENTS)).toBe("Alpha, Beta");
  });
});

describe("previews and results", () => {
  const diff: SourceDiff = {
    id: "x",
    ref: "v2",
    from: C1,
    commit: "89abcdef0123456789abcdef0123456789abcdef",
    changed: true,
    added: [{ name: "triage", path: "skills/triage", description: "T", attached: false }],
    removed: [
      { name: "changelog", path: "skills/changelog", attached: true },
      { name: "old", path: "skills/old", attached: false },
    ],
    moved: [],
    described: [
      { name: "notes", path: "skills/notes", error: "SKILL.md has no description", attached: true },
    ],
  };

  it("reads previews", () => {
    expect(diffOf(job(diff))).toEqual(diff);
    expect(diffOf(job({ commit: C1, found: {}, id: "x", exists: "" }))).toBeNull();
    expect(newScanOf(job({ commit: C1, found: {}, id: "x", exists: "" }))).toMatchObject({
      id: "x",
    });
    expect(newScanOf(job(diff))).toBeNull();
    expect(newScanOf(job(null))).toBeNull();
  });

  it("lists the attached skills an update leaves missing", () => {
    expect(dropsAttached(diff)).toEqual(["changelog", "notes"]);
  });

  it("words the toasts", () => {
    expect(addedText({ id: "x", attached: ["a", "b"], skipped: ["c"] })).toBe(
      "x added; 2 skills attached to all agents. Skipped c: another source already provides it.",
    );
    expect(addedText({ id: "x", attached: [], skipped: [] })).toBe("x added.");
    expect(updatedText({ changed: false })).toBe("Already up to date.");
    expect(updatedText({ id: "x", commit: C1, changed: true, missing: [] })).toBe(
      "x updated to 0123456789ab.",
    );
    expect(updatedText({ id: "x", commit: C1, changed: true, missing: ["a", "b"] })).toBe(
      "x updated to 0123456789ab. a, b are no longer in the source; detach them on its page.",
    );
  });

  it("checks a source id", () => {
    expect(sourceIdError("acme-tools")).toBe("");
    expect(sourceIdError("Acme")).not.toBe("");
    expect(sourceIdError("a".repeat(65))).not.toBe("");
  });
});
