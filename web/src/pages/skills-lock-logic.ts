import type { LockAddResult, LockSkill } from "@/api/types";
import { count } from "@/lib/format";
import { shortCommit, sourceTitle } from "./skills-logic";

/** LockState is how one skill of a workspace's skills-lock.json stands. */
export interface LockState {
  label: string;
  tone: "ok" | "warn" | "info" | "idle";
  addable: boolean;
}

/** lockState says whether Berth gives the skill already, the workspace
 * holds it, Berth cannot add it, or it can be added. */
export function lockState(s: LockSkill): LockState {
  if (s.provider) return { label: `From ${s.provider}`, tone: "ok", addable: false };
  switch (s.installed) {
    case "match":
      return { label: "In .agents/skills, as the lock pins it", tone: "ok", addable: false };
    case "differs":
      return {
        label: "In .agents/skills, but its files differ from the lock",
        tone: "warn",
        addable: false,
      };
    case "unverified":
      return { label: "In .agents/skills", tone: "idle", addable: false };
  }
  if (!s.repo) return { label: s.reason ?? "Berth cannot add it", tone: "idle", addable: false };
  return { label: "Not installed", tone: "info", addable: true };
}

/** lockOrigin is where the lock says a skill comes from: owner/repo, its
 * folder and pinned commit, or a website's host. */
export function lockOrigin(s: LockSkill): string {
  if (!s.repo) return s.source;
  if (s.kind === "well-known") return sourceTitle({ kind: s.kind, repo: s.repo });
  const where = [sourceTitle({ repo: s.repo }), s.folder].filter(Boolean).join(" · ");
  return s.commit ? `${where} @ ${shortCommit(s.commit)}` : `${where} @ ${s.ref ?? "HEAD"}`;
}

/** addableNames lists the skills Add would install. */
export const addableNames = (skills: LockSkill[]) =>
  skills.filter((s) => lockState(s).addable).map((s) => s.name);

/** lockAddedText sums up a workspaces.skills_lock.add. */
export function lockAddedText(r: LockAddResult, id: string): string {
  const added = r.added?.length ?? 0;
  const parts = [
    added ? `${count(added, "skill", "skills")} added to ${id}` : `Nothing added to ${id}`,
  ];
  if (r.sources?.length)
    parts.push(`new ${r.sources.length === 1 ? "source" : "sources"} ${r.sources.join(", ")}`);
  if (r.skipped?.length)
    parts.push(`${count(r.skipped.length, "skill", "skills")} left out; see Activity`);
  return `${parts.join("; ")}.`;
}
