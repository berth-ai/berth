import { describe, expect, it } from "vitest";
import type { AgentPackage, Attachment, Source, WorkspaceReport } from "@/api/types";
import {
  findable,
  mcpFileNote,
  mcpTargetsText,
  mcpWorkspacesPatch,
  skillState,
  strandedSkills,
  targetsText,
  toggled,
  trustNote,
  workspaceSkills,
  wsConflicts,
  wsPatch,
} from "./attachments-logic";

const agent = (pkg: string, name: string): AgentPackage => ({
  package: pkg,
  name,
  kind: "agent",
  version: "1",
  enabled: true,
  bins: [],
  keys: [],
});
const AGENTS = [agent("a", "Alpha"), agent("b", "Beta")];

const src = (attached: Record<string, Attachment>): Source => ({
  repo: "https://github.com/acme/skills",
  ref: "main",
  path: "",
  commit: "0123456789abcdef0123456789abcdef01234567",
  found: {
    pdf: { path: "pdf", description: "PDF tools" },
    docx: { path: "docx", description: "Word files" },
    bad: { path: "bad", error: "no description" },
  },
  attached,
});

const SOURCES = {
  x: src({ pdf: { enabled: true, agents: [], workspaces: { api: "link" } } }),
  y: src({
    docx: { enabled: false, agents: null, workspaces: { api: "vendor", web: "link" } },
    gone: { enabled: true, workspaces: { api: "link" } },
  }),
};

describe("workspace attachments", () => {
  it("finds the source that already gives a name to a workspace", () => {
    expect(wsConflicts(SOURCES, "y", "pdf")).toEqual({ api: "x" });
    expect(wsConflicts(SOURCES, "x", "pdf")).toEqual({});
  });

  it("names agents and workspaces", () => {
    expect(targetsText({ enabled: true, agents: [], workspaces: { api: "link" } }, AGENTS)).toBe(
      "api",
    );
    expect(
      targetsText(
        { enabled: true, agents: null, workspaces: { web: "link", api: "vendor" } },
        AGENTS,
      ),
    ).toBe("All agents · api (vendored), web");
    expect(targetsText({ enabled: true, agents: [] }, AGENTS)).toBe("No agent");
  });

  it("attaches to and detaches from one workspace", () => {
    const at = (v: unknown) => ({ skills: { sources: { x: { attached: { pdf: v } } } } });
    expect(wsPatch("x", "pdf", "api", "vendor", null)).toEqual(
      at({ enabled: true, agents: [], workspaces: { api: "vendor" } }),
    );
    expect(wsPatch("x", "pdf", "web", "link", SOURCES.x.attached!.pdf)).toEqual(
      at({ workspaces: { web: "link" } }),
    );
    expect(wsPatch("x", "pdf", "api", null, SOURCES.x.attached!.pdf)).toEqual(at(null));
    expect(
      wsPatch("x", "pdf", "api", null, {
        enabled: true,
        agents: null,
        workspaces: { api: "link" },
      }),
    ).toEqual(at({ workspaces: { api: null } }));
  });

  it("lists a workspace's skills, missing ones flagged", () => {
    const rows = workspaceSkills(SOURCES, "api");
    expect(rows.map((r) => [r.key, r.mode, r.missing])).toEqual([
      ["y/docx", "vendor", false],
      ["y/gone", "link", true],
      ["x/pdf", "link", false],
    ]);
  });

  it("says what Berth did with each skill", () => {
    const [docx, gone, pdf] = workspaceSkills(SOURCES, "api");
    const report: WorkspaceReport = {
      skills: {
        entries: {
          "x/pdf": { name: "pdf", mode: "link", links: [".claude/skills/pdf"] },
          "z/old": { name: "old", mode: "vendor", dir: ".agents/skills/old", edited: true },
        },
        skipped: {},
      },
      mcp: {},
    };
    expect(skillState(pdf!, report)).toMatchObject({ label: "Linked", tone: "ok" });
    expect(skillState(docx!, report)).toMatchObject({ label: "Off", tone: "idle" });
    expect(skillState(gone!, report)).toMatchObject({ label: "Missing from source", tone: "warn" });
    const on = { ...docx!, attachment: { ...docx!.attachment, enabled: true } };
    expect(skillState(on, report)).toMatchObject({ label: "Waiting", tone: "info" });
    expect(
      skillState(on, {
        skills: { entries: { "y/docx": { name: "docx", mode: "vendor", edited: true } } },
        mcp: {},
      }),
    ).toMatchObject({
      label: "Local edits",
      edited: true,
    });
    expect(
      skillState(on, {
        skills: { skipped: { ".agents/skills/docx": "already exists; Berth left it alone" } },
        mcp: {},
      }),
    ).toMatchObject({
      label: "Already in the folder",
    });
    expect(strandedSkills(report, workspaceSkills(SOURCES, "api")).map((r) => r.key)).toEqual([
      "z/old",
    ]);
  });

  it("finds attachable skills across sources", () => {
    const rows = findable(SOURCES, "api", "");
    expect(rows.map((r) => `${r.sourceId}/${r.name}:${r.taken}:${r.attached}`)).toEqual([
      "x/docx:y:false",
      "y/docx::true",
      "x/pdf::true",
      "y/pdf:x:false",
    ]);
    expect(findable(SOURCES, "api", "word").map((r) => r.name)).toEqual(["docx", "docx"]);
  });

  it("toggles a server's workspaces", () => {
    expect(toggled(["b"], "a", true)).toEqual(["a", "b"]);
    expect(toggled(["a"], "a", false)).toEqual([]);
    expect(mcpWorkspacesPatch("c7", [])).toEqual({ mcp: { workspaces: { c7: null } } });
    expect(mcpWorkspacesPatch("c7", ["b", "a"])).toEqual({
      mcp: { workspaces: { c7: ["a", "b"] } },
    });
    expect(mcpTargetsText(undefined, ["api"], AGENTS)).toBe("All agents · api");
    expect(mcpTargetsText([], [], AGENTS)).toBe("No installed agent");
  });

  it("explains each project file", () => {
    expect(mcpFileNote({ style: "claude", ids: ["c7"], created: true })).toMatch(/hidden from git/);
    expect(mcpFileNote({ style: "claude", ids: ["c7"], tracked: true })).toMatch(/git diff/);
    expect(mcpFileNote({ style: "claude", ids: ["c7"] })).toMatch(/merges/);
  });

  it("says which agents will trust a workspace", () => {
    const codex = { ...agent("c", "Codex"), mcp_trust: true };
    expect(trustNote([...AGENTS, codex])).toBe("Codex will trust each workspace you tick.");
    expect(trustNote([...AGENTS, { ...codex, enabled: false }])).toBe("");
  });
});
