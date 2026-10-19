import { describe, expect, it } from "vitest";
import type { AgentPackage, InstructionFile } from "@/api/types";
import { fileState, instructionRows, mergeable } from "./instructions-logic";

const pkg = (name: string, instructions: string, enabled = true): AgentPackage =>
  ({ package: name, name, kind: "agent", enabled, instructions }) as AgentPackage;

describe("instructionRows", () => {
  const pkgs = [
    pkg("Claude", "CLAUDE.md"),
    pkg("Codex", "AGENTS.md"),
    pkg("OpenCode", "AGENTS.md"),
    pkg("Gemini", "GEMINI.md", false),
  ];
  it("lists each enabled agent's file once, with its readers", () => {
    expect(instructionRows(pkgs, false)).toEqual([
      { file: "AGENTS.md", agents: ["Codex", "OpenCode"] },
      { file: "CLAUDE.md", agents: ["Claude"] },
    ]);
  });
  it("adds AGENTS.md while shared, with every agent reading it", () => {
    expect(instructionRows([pkg("Claude", "CLAUDE.md")], true)).toEqual([
      { file: "AGENTS.md", agents: ["Claude"] },
      { file: "CLAUDE.md", agents: ["Claude"] },
    ]);
  });
});

describe("fileState", () => {
  const f = (over: Partial<InstructionFile>): InstructionFile => ({
    file: "CLAUDE.md",
    state: "file",
    content: "",
    sha256: "x",
    size: 1,
    editable: true,
    tracked: false,
    ...over,
  });
  it("names each state", () => {
    expect(fileState(f({ state: "missing" }))).toEqual({ label: "Not created yet", tone: "muted" });
    expect(fileState(f({ state: "symlink", target: "AGENTS.md", owned: true }))).toEqual({
      label: "Shares AGENTS.md",
      tone: "ok",
    });
    expect(fileState(f({ state: "symlink", target: "docs/AI.md" }))).toEqual({
      label: "Link to docs/AI.md",
      tone: "muted",
    });
    expect(fileState(f({ tracked: true }))).toEqual({ label: "Tracked by git", tone: "muted" });
    expect(fileState(f({ editable: false, reason: "too big" }))).toEqual({
      label: "too big",
      tone: "warn",
    });
  });
});

describe("mergeable", () => {
  it("lists the files with their own content", () => {
    expect(
      mergeable({
        skipped: {
          "CLAUDE.md": "CLAUDE.md has its own content",
          "GEMINI.md": "AGENTS.md is missing; write it or merge a file into it",
        },
      }),
    ).toEqual(["CLAUDE.md"]);
    expect(mergeable(undefined)).toEqual([]);
  });
});
