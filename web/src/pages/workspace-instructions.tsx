import { useId, useState } from "react";
import {
  keys,
  queryClient,
  readInstructionFile,
  refreshWorkspaceReport,
  useConfig,
  useInstructionFile,
  useStatus,
  useWorkspaceReport,
} from "@/api/queries";
import { ApiError } from "@/api/request";
import type { InstructionFile } from "@/api/types";
import { mergeInstructions, writeInstructions } from "@/api/workspaces";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { OnOffSwitch } from "@/components/on-off-switch";
import { PanelCard } from "@/components/panel-card";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { cn } from "@/lib/utils";
import { applyAndNotify } from "./apply-notify";
import { fileState, instructionRows, mergeable, SHARED, type Tone } from "./instructions-logic";

const toneClass: Record<Tone, string> = {
  ok: "text-ok",
  warn: "text-warn",
  muted: "text-fg-muted",
};

/** WorkspaceInstructions is a workspace's Instructions tab (DESIGN.md §4). */
export function WorkspaceInstructions({ id }: { id: string }) {
  const config = useConfig();
  const status = useStatus();
  const report = useWorkspaceReport(id);
  const ws = config.data?.config.workspaces?.[id];
  const packages = status.data?.packages;
  if (!ws || !packages) return <LoadingRows n={2} />;
  const shared = ws.instructions?.shared ?? false;
  const rows = instructionRows(packages, shared);
  const section = report.data?.instructions;
  const merge = mergeable(section);
  return (
    <>
      <PanelCard title="Shared instructions">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-fg-secondary">
            Every agent reads {SHARED}: Berth makes each other file a link to it, hidden from git.
          </p>
          <OnOffSwitch
            label={`Agents in ${id} share ${SHARED}`}
            checked={shared}
            onCheckedChange={(on) =>
              void applyAndNotify(
                { workspaces: { [id]: { instructions: { shared: on } } } },
                on
                  ? `Agents in ${id} now share ${SHARED}.`
                  : `Agents in ${id} read their own files again.`,
              ).then(() => refreshWorkspaceReport(id))
            }
          />
        </div>
        {section?.error ? <InlineAlert tone="bad">{section.error}</InlineAlert> : null}
        {Object.entries(section?.skipped ?? {})
          .filter(([f]) => !merge.includes(f))
          .map(([f, why]) => (
            <InlineAlert key={f} tone="warn">
              {why}
            </InlineAlert>
          ))}
        {merge.map((f) => (
          <MergeAlert key={f} id={id} file={f} />
        ))}
      </PanelCard>
      {rows.map((r) => (
        <InstructionCard key={r.file} id={id} file={r.file} agents={r.agents} />
      ))}
      <p className="text-xs text-fg-muted">
        Git versions these files; Berth keeps no revisions of its own.
      </p>
    </>
  );
}

function InstructionCard({ id, file, agents }: { id: string; file: string; agents: string[] }) {
  const q = useInstructionFile(id, file);
  if (!q.data) return <LoadingRows n={1} />;
  // A new state (a link appeared, say) starts a fresh editor.
  return (
    <InstructionEditor
      key={`${file}:${q.data.state}`}
      id={id}
      file={file}
      agents={agents}
      initial={q.data}
    />
  );
}

/** InstructionEditor edits one file. It keeps the version its draft is
 * based on; a save over a changed file keeps the draft and offers to use
 * it anyway or to take the file on disk. */
export function InstructionEditor({
  id,
  file,
  agents,
  initial,
}: {
  id: string;
  file: string;
  agents: string[];
  initial: InstructionFile;
}) {
  const inputId = useId();
  const [draft, setDraft] = useState(initial.content);
  const [saved, setSaved] = useState(initial.content);
  const [base, setBase] = useState(initial.sha256);
  const [busy, setBusy] = useState(false);
  const [conflict, setConflict] = useState<InstructionFile | null>(null);
  const state = fileState(initial);

  const save = async (over: string) => {
    setBusy(true);
    try {
      const sum = await writeInstructions(id, file, draft, over);
      setBase(sum);
      setSaved(draft);
      setConflict(null);
      notify(`${file} saved.`);
      void queryClient.invalidateQueries({ queryKey: keys.instructions(id, file) });
    } catch (err) {
      if (err instanceof ApiError && err.code === "changed_on_disk") {
        try {
          setConflict(await readInstructionFile(id, file));
        } catch (readErr) {
          notify(errorText(readErr, "The file could not be read again."), "bad");
        }
      } else {
        notify(errorText(err, "The save failed."), "bad");
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <PanelCard title={file}>
      {agents.length ? <p className="text-xs text-fg-muted">Read by {agents.join(", ")}</p> : null}
      <span className={cn("text-xs", toneClass[state.tone])}>{state.label}</span>
      {initial.editable ? (
        <>
          <label htmlFor={inputId} className="sr-only">
            {file}
          </label>
          <Textarea
            id={inputId}
            value={draft}
            spellCheck={false}
            className="min-h-64 font-mono text-xs"
            onChange={(e) => setDraft(e.target.value)}
          />
          {conflict ? (
            <InlineAlert tone="warn">
              <div className="flex flex-col gap-2">
                <span>{file} changed on disk since you opened it. Your edits are still here.</span>
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" disabled={busy} onClick={() => void save(conflict.sha256)}>
                    Use my version
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={() => {
                      setDraft(conflict.content);
                      setSaved(conflict.content);
                      setBase(conflict.sha256);
                      setConflict(null);
                    }}
                  >
                    Discard my edits
                  </Button>
                </div>
              </div>
            </InlineAlert>
          ) : null}
          <div className="flex justify-end">
            <BusyButton
              size="sm"
              busy={busy}
              busyLabel="Saving…"
              disabled={draft === saved || conflict !== null}
              aria-label={`Save ${file}`}
              onClick={() => void save(base)}
            >
              Save
            </BusyButton>
          </div>
        </>
      ) : null}
    </PanelCard>
  );
}

function MergeAlert({ id, file }: { id: string; file: string }) {
  const [dialog, confirm] = useConfirm();
  return (
    <InlineAlert tone="warn">
      <div className="flex flex-wrap items-center gap-2">
        <span>
          {file} has its own content, so it does not share {SHARED} yet.
        </span>
        <Button
          size="sm"
          variant="outline"
          onClick={() =>
            confirm({
              title: `Append ${file} to ${SHARED}?`,
              body: `Berth adds ${file}'s text to the end of ${SHARED}, then replaces ${file} with a link to it. If git tracks ${file}, the change shows in git status.`,
              confirm: "Append and replace",
              action: async () => {
                try {
                  await mergeInstructions(id, file);
                } catch (err) {
                  notify(errorText(err), "bad");
                  return false;
                }
                notify(`${file} now shares ${SHARED}.`);
                void queryClient.invalidateQueries({ queryKey: ["instructions", id] });
                void refreshWorkspaceReport(id);
              },
            })
          }
        >
          Append to {SHARED} and replace
        </Button>
        {dialog}
      </div>
    </InlineAlert>
  );
}
