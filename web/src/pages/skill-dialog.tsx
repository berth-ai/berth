import { useId, useState } from "react";
import { applyConfig } from "@/api/config";
import { keys, queryClient } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { AgentPackage, Job, Skill } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LogViewer } from "@/components/log-viewer";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { AgentChecklist } from "./available-picker";
import { CATALOG, type CatalogEntry } from "./skills-catalog";
import {
  agentsPatch,
  normalizeRepo,
  pathError,
  refError,
  repoError,
  viaNotes,
} from "./skills-logic";

export interface EditingSkill {
  id: string;
  skill: Skill;
}

type Tab = "catalog" | "repo";
type Errors = Partial<Record<"repo" | "path" | "ref" | "form", string>>;

const BG = " Agents pick it up in the background.";

/** SkillDialog adds a skill from the catalog or a repository, or edits one's ref and agents. */
export function SkillDialog({
  agents,
  unsupported,
  installed,
  editing,
  onClose,
}: {
  agents: AgentPackage[];
  unsupported: AgentPackage[];
  installed: string[];
  editing: EditingSkill | null;
  onClose: () => void;
}) {
  const [tab, setTab] = useState<Tab>("catalog");
  const [repo, setRepo] = useState("");
  const [path, setPath] = useState("");
  const [ref, setRef] = useState(editing?.skill.ref ?? "main");
  const [picked, setPicked] = useState<string[] | null>(editing?.skill.agents ?? null);
  const [errors, setErrors] = useState<Errors>({});
  const [job, setJob] = useState<Job | null>(null);
  const [busy, setBusy] = useState(false);
  const ids = { repo: useId(), path: useId(), ref: useId() };

  const finish = (done: Job | null, okText: string) => {
    setBusy(false);
    void queryClient.invalidateQueries({ queryKey: keys.config });
    if (!done) return;
    if (done.state === "succeeded") {
      notify(done.result?.changed === false ? "Already up to date." : okText);
      onClose();
      return;
    }
    const err = done.result?.error;
    const field = err?.field;
    const key = field === "repo" || field === "path" || field === "ref" ? field : "form";
    setErrors({ [key]: err?.message ?? "The job failed; open Activity for its log." });
  };

  const start = async (body: object, verb: string, okText: string) => {
    setBusy(true);
    setErrors({});
    finish(await runJob(verb, body, setJob), okText);
  };

  const addFromRepo = () => {
    const e: Errors = {
      repo: repoError(repo),
      path: pathError(path.trim()),
      ref: refError(ref.trim()),
    };
    if (e.repo || e.path || e.ref) {
      setErrors(e);
      return;
    }
    void start(
      { repo: normalizeRepo(repo), path: path.trim(), ref: ref.trim(), agents: picked },
      "skills.add",
      `Skill added.${BG}`,
    );
  };

  const addFromCatalog = (c: CatalogEntry) =>
    void start(
      { repo: c.repo, path: c.path, ref: c.ref, agents: picked },
      "skills.add",
      `${c.id} added.${BG}`,
    );

  const saveEdit = async () => {
    if (!editing) return;
    const r = ref.trim();
    const e = refError(r);
    if (e) {
      setErrors({ ref: e });
      return;
    }
    if (JSON.stringify(picked) !== JSON.stringify(editing.skill.agents)) {
      setBusy(true);
      try {
        await applyConfig(agentsPatch(editing.id, picked));
      } catch (err) {
        setBusy(false);
        setErrors({ form: errorText(err) });
        return;
      }
    }
    if (r !== editing.skill.ref) {
      await start({ id: editing.id, ref: r }, "skills.update", `${editing.id} moved to ${r}.${BG}`);
      return;
    }
    setBusy(false);
    void queryClient.invalidateQueries({ queryKey: keys.config });
    notify(`${editing.id} saved.${BG}`);
    onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${editing.id}` : "Add a skill"}</DialogTitle>
        </DialogHeader>
        {editing ? null : (
          <>
            <p className="text-xs text-fg-muted">
              Skills are instructions and scripts your agents run. Add only sources you trust.
            </p>
            <ToggleGroup
              aria-label="Skill source"
              value={[tab]}
              onValueChange={(v) => {
                if (v[0] !== undefined) setTab(v[0] as Tab);
              }}
              className="w-full gap-0.5 rounded-md bg-surface-sunken p-0.5"
            >
              {(
                [
                  ["catalog", "Catalog"],
                  ["repo", "From a repository"],
                ] as const
              ).map(([value, label]) => (
                <ToggleGroupItem
                  key={value}
                  value={value}
                  className="h-7 flex-1 rounded-sm px-3 font-medium text-fg-muted data-pressed:bg-surface data-pressed:text-fg data-pressed:shadow-xs"
                >
                  {label}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </>
        )}
        {!editing && tab === "catalog" ? (
          <ul className="flex max-h-72 flex-col overflow-y-auto" aria-label="Catalog">
            {CATALOG.map((c) => {
              const have = installed.includes(c.id);
              return (
                <li
                  key={c.id}
                  className="flex items-center justify-between gap-3 border-b border-line py-2 last:border-b-0"
                >
                  <span className="flex min-w-0 flex-col">
                    <span className="font-mono text-xs font-semibold text-fg">{c.id}</span>
                    <span className="text-xs text-fg-muted">{c.description}</span>
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={have || busy}
                    aria-label={`Add ${c.id}`}
                    onClick={() => addFromCatalog(c)}
                  >
                    {have ? "Installed" : "Add"}
                  </Button>
                </li>
              );
            })}
          </ul>
        ) : null}
        {!editing && tab === "repo" ? (
          <>
            <FormField
              id={ids.repo}
              label="Repository"
              hint="owner/repo for GitHub, or an https URL."
              error={errors.repo}
            >
              <Input
                {...fieldProps(ids.repo, errors.repo)}
                value={repo}
                className="font-mono text-xs"
                spellCheck={false}
                autoComplete="off"
                onChange={(ev) => setRepo(ev.target.value)}
              />
            </FormField>
            <FormField
              id={ids.path}
              label="Path"
              hint="The folder holding SKILL.md; empty for the root."
              error={errors.path}
            >
              <Input
                {...fieldProps(ids.path, errors.path)}
                value={path}
                className="font-mono text-xs"
                spellCheck={false}
                autoComplete="off"
                onChange={(ev) => setPath(ev.target.value)}
              />
            </FormField>
          </>
        ) : null}
        {editing || tab === "repo" ? (
          <FormField
            id={ids.ref}
            label="Ref"
            hint="A branch, tag or commit. Berth pins it to a commit."
            error={errors.ref}
          >
            <Input
              {...fieldProps(ids.ref, errors.ref)}
              value={ref}
              className="font-mono text-xs"
              spellCheck={false}
              autoComplete="off"
              onChange={(ev) => setRef(ev.target.value)}
            />
          </FormField>
        ) : null}
        <AgentChecklist
          value={picked}
          agents={agents}
          unsupported={unsupported}
          notes={viaNotes(picked, agents)}
          disabled={busy}
          onChange={setPicked}
        />
        {errors.form ? <InlineAlert tone="bad">{errors.form}</InlineAlert> : null}
        {job?.log ? <LogViewer text={job.log} truncated={job.log_truncated} /> : null}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          {editing ? (
            <BusyButton busy={busy} busyLabel="Saving…" onClick={() => void saveEdit()}>
              Save
            </BusyButton>
          ) : tab === "repo" ? (
            <BusyButton busy={busy} busyLabel="Adding…" onClick={addFromRepo}>
              Add skill
            </BusyButton>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
