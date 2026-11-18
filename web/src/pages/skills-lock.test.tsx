import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Job, LockSkill } from "@/api/types";
import { SkillsLockCard } from "./skills-lock";
import { addableNames, lockAddedText, lockOrigin, lockState } from "./skills-lock-logic";

const lockData = vi.hoisted(() => ({ current: null as unknown }));
vi.mock("@/api/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/queries")>()),
  refreshWorkspaceReport: vi.fn(),
  useSkillsLock: () => ({ data: lockData.current, error: null }),
}));
vi.mock("@/api/run-job", () => ({ runJob: vi.fn() }));

const C = "0123456789abcdef0123456789abcdef01234567";
const row = (over: Partial<LockSkill>): LockSkill => ({
  name: "pdf",
  source: "acme/skills",
  source_type: "github",
  repo: "https://github.com/acme/skills",
  ref: "main",
  commit: C,
  folder: "skills/pdf",
  installed: "absent",
  ...over,
});
const ROWS = [
  row({}),
  row({ name: "docx", provider: "acme-skills" }),
  row({ name: "present", installed: "match" }),
  row({ name: "edited", installed: "differs" }),
  row({
    name: "local",
    repo: undefined,
    source: "./x",
    source_type: "local",
    reason: "Berth cannot add skills from a local source",
  }),
  row({
    name: "guide",
    kind: "well-known",
    repo: "https://docs.example.org",
    commit: undefined,
    folder: "",
  }),
];

describe("skills-lock logic", () => {
  it("states each skill and lists the addable ones", () => {
    expect(ROWS.map((r) => lockState(r).label)).toEqual([
      "Not installed",
      "From acme-skills",
      "In .agents/skills, as the lock pins it",
      "In .agents/skills, but its files differ from the lock",
      "Berth cannot add skills from a local source",
      "Not installed",
    ]);
    expect(addableNames(ROWS)).toEqual(["pdf", "guide"]);
  });

  it("names where the lock pins a skill", () => {
    expect(lockOrigin(ROWS[0])).toBe("acme/skills · skills/pdf @ 0123456789ab");
    expect(lockOrigin(row({ commit: undefined, ref: "HEAD" }))).toBe(
      "acme/skills · skills/pdf @ HEAD",
    );
    expect(lockOrigin(ROWS[5])).toBe("docs.example.org");
    expect(lockOrigin(ROWS[4])).toBe("./x");
  });

  it("sums up an add", () => {
    expect(
      lockAddedText(
        { added: [{ name: "pdf", source: "acme-skills" }], sources: ["acme-skills"], skipped: [] },
        "api",
      ),
    ).toBe("1 skill added to api; new source acme-skills.");
    expect(lockAddedText({ added: [], skipped: [{ name: "x", reason: "r" }] }, "api")).toBe(
      "Nothing added to api; 1 skill left out; see Activity.",
    );
  });
});

describe("SkillsLockCard", () => {
  beforeEach(async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockReset();
  });

  it("shows nothing without a lock file", () => {
    lockData.current = { found: false, skills: [] };
    const { container } = render(<SkillsLockCard id="api" readOnly={false} />);
    expect(container.textContent).toBe("");
  });

  it("adds the skills the workspace lacks after confirming", async () => {
    const { runJob } = await import("@/api/run-job");
    vi.mocked(runJob).mockResolvedValue({
      state: "succeeded",
      result: {
        added: [{ name: "pdf", source: "acme-skills" }],
        sources: ["acme-skills"],
        skipped: [],
      },
    } as unknown as Job);
    lockData.current = { found: true, version: 2, skills: ROWS };
    render(<SkillsLockCard id="api" readOnly={false} />);
    expect(screen.getByText("From acme-skills")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add 2 skills" }));
    expect(await screen.findByText(/Berth links pdf, guide into api/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await vi.waitFor(() =>
      expect(runJob).toHaveBeenCalledWith("workspaces.skills_lock.add", {
        id: "api",
        names: ["pdf", "guide"],
      }),
    );
  });

  it("offers no Add when nothing is missing", () => {
    lockData.current = { found: true, version: 2, skills: [ROWS[1], ROWS[2]] };
    render(<SkillsLockCard id="api" readOnly={false} />);
    expect(screen.queryByRole("button", { name: /^Add/ })).toBeNull();
  });
});
