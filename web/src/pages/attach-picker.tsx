import { useState } from "react";
import type { AgentPackage, Attachment, SkillMode } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { Segmented } from "@/components/segmented";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { WorkspaceChecklist } from "@/components/workspace-checklist";
import { cn } from "@/lib/utils";
import { MODE_LABELS, workspacesOf } from "./attachments-logic";
import { viaNotes } from "./skills-logic";
import { agentsOf, pickAgent } from "./sources-logic";

/** AttachPicker chooses who gets one skill of a source (DESIGN.md §4).
 * "All agents, including ones added later" is null; unticking it lists
 * today's agents. An agent another source already gives the skill to is
 * locked with that source's name; one that sees the skill through another
 * agent's folder shows ticked and locked. Saving no agent and no
 * workspace detaches. Workspaces get the skill linked (hidden from git) or
 * vendored (copied in). */
export function AttachPicker({
  name,
  source,
  attachment,
  agents,
  unsupported,
  taken,
  workspaces,
  wsTaken,
  onSave,
  onClose,
}: {
  name: string;
  source: string;
  attachment: Attachment | null;
  agents: AgentPackage[];
  unsupported: AgentPackage[];
  taken: Record<string, string>;
  workspaces: string[];
  wsTaken: Record<string, string>;
  onSave: (agents: string[] | null, workspaces: Record<string, SkillMode>) => Promise<boolean>;
  onClose: () => void;
}) {
  const all = agents.map((a) => a.package);
  const blocked = Object.keys(taken).length > 0;
  const [value, setValue] = useState<string[] | null>(() => {
    if (attachment) return agentsOf(attachment);
    return blocked ? all.filter((p) => !taken[p]) : null;
  });
  const [busy, setBusy] = useState(false);
  const [ws, setWs] = useState<Record<string, SkillMode>>(() => ({ ...workspacesOf(attachment) }));
  const ticked = new Set(value ?? all);
  const through = viaNotes(value, agents);
  const none = value !== null && value.length === 0 && Object.keys(ws).length === 0;
  const verb = attachment ? (none ? "Detach" : "Save") : "Attach";

  const save = async () => {
    setBusy(true);
    const ok = await onSave(value, ws);
    setBusy(false);
    if (ok) onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{attachment ? `Who gets ${name}` : `Attach ${name}`}</DialogTitle>
        </DialogHeader>
        <p className="text-xs text-fg-muted">
          From <span className="font-mono">{source}</span>. Each agent loads it from its skills
          folder; a workspace gets it in its agents' project folders.
        </p>
        <fieldset className="flex flex-col gap-2" disabled={busy}>
          <legend className="mb-1 font-medium">Agents</legend>
          <Label className="flex flex-wrap items-center gap-2 leading-[inherit] font-normal">
            <Checkbox
              checked={value === null}
              disabled={busy || blocked}
              onCheckedChange={(on) => setValue(on ? null : all)}
            />
            All agents, including ones added later
            {blocked ? (
              <span className="text-xs text-fg-muted">
                Another source gives {name} to some agents
              </span>
            ) : null}
          </Label>
          {agents.map((a) => {
            const other = taken[a.package];
            const note = other ? `Gets ${name} from ${other}` : through[a.package];
            const on = !other && (ticked.has(a.package) || Boolean(through[a.package]));
            return (
              <Label
                key={a.package}
                className={cn(
                  "flex flex-wrap items-center gap-2 leading-[inherit] font-normal",
                  other && "text-fg-muted",
                )}
              >
                <Checkbox
                  checked={on}
                  disabled={busy || Boolean(note)}
                  onCheckedChange={(c) => setValue(pickAgent(value, all, a.package, c))}
                />
                {a.name}
                {note ? <span className="text-xs text-fg-muted">{note}</span> : null}
              </Label>
            );
          })}
          {unsupported.map((a) => (
            <Label
              key={a.package}
              className="flex items-center gap-2 leading-[inherit] font-normal text-fg-muted"
            >
              <Checkbox checked={false} disabled />
              {a.name}
              <span className="text-xs">Doesn't support skills</span>
            </Label>
          ))}
        </fieldset>
        <WorkspaceChecklist
          workspaces={workspaces}
          checked={Object.keys(ws)}
          taken={wsTaken}
          takenText={(other) => `Gets ${name} from ${other}`}
          disabled={busy}
          onToggle={(id, on) =>
            setWs((cur) => {
              const next = { ...cur };
              if (on) next[id] = "link";
              else delete next[id];
              return next;
            })
          }
          extra={(id) => (
            <Segmented
              size="sm"
              label={`How ${id} gets ${name}`}
              value={ws[id] ?? "link"}
              onChange={(m) => setWs((cur) => ({ ...cur, [id]: m }))}
              options={(["link", "vendor"] as const).map((m) => ({
                value: m,
                label: MODE_LABELS[m],
              }))}
              className="w-36"
            />
          )}
          note="Link keeps the skill in Berth's store and hides the link from git; it works only inside the container. Vendor copies the files into the repository for you to commit."
        />
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <BusyButton
            busy={busy}
            busyLabel={`${verb === "Attach" ? "Attaching" : verb === "Save" ? "Saving" : "Detaching"}…`}
            disabled={!attachment && none}
            onClick={() => void save()}
          >
            {verb}
          </BusyButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
