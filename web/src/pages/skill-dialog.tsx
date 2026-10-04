import { useId, useState } from "react";
import { applyConfig } from "@/api/config";
import { keys, queryClient } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { AgentPackage, Job, ScanResult } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LogViewer } from "@/components/log-viewer";
import { Segmented } from "@/components/segmented";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { errorText } from "@/lib/errors";
import { count } from "@/lib/format";
import { notify } from "@/lib/notify";
import { AgentChecklist } from "./available-picker";
import { ScanChecklist } from "./scan-checklist";
import { CATALOG, type CatalogEntry } from "./skills-catalog";
import {
  agentsPatch,
  defaultPicks,
  normalizeRepo,
  pathError,
  refError,
  repoError,
  repoLabel,
  scanOf,
  type SkillRow,
  viaNotes,
} from "./skills-logic";

export type EditingSkill = SkillRow;

type Tab = "catalog" | "repo";
type Errors = Partial<Record<"repo" | "path" | "ref" | "form", string>>;

const BG = " Agents pick it up in the background.";

/** SkillDialog adds skills from the catalog or a repository, or edits one's
 * ref and agents; group is the edited skill's group ([] when adding). */
export function SkillDialog({
  agents,
  unsupported,
  installed,
  editing,
  group,
  onClose,
}: {
  agents: AgentPackage[];
  unsupported: AgentPackage[];
  installed: string[];
  editing: EditingSkill | null;
  group: SkillRow[];
  onClose: () => void;
}) {
  const [tab, setTab] = useState<Tab>("catalog");
  const [repo, setRepo] = useState("");
  const [path, setPath] = useState("");
  const [ref, setRef] = useState(editing?.skill.ref ?? "main");
  const [scan, setScan] = useState<ScanResult | null>(null);
  const [picks, setPicks] = useState<string[]>([]);
  const [applyAll, setApplyAll] = useState(true);
  const [picked, setPicked] = useState<string[] | null>(editing?.skill.agents ?? null);
  const [errors, setErrors] = useState<Errors>({});
  const [job, setJob] = useState<Job | null>(null);
  const [busy, setBusy] = useState(false);
  const ids = { repo: useId(), path: useId(), ref: useId() };
  const grouped = editing !== null && group.length > 1;

  // run starts a job and returns it if it succeeded; a failure lands on its
  // field when that field is on screen, else under the form.
  const run = async (verb: string, body: object): Promise<Job | null> => {
    setErrors({});
    const finished = await runJob(verb, body, setJob);
    if (!finished || finished.state === "succeeded") return finished;
    const err = finished.result?.error;
    const shown = editing ? ["ref"] : tab === "repo" && !scan ? ["repo", "path", "ref"] : [];
    const key = err?.field && shown.includes(err.field) ? (err.field as keyof Errors) : "form";
    setErrors({ [key]: err?.message ?? "The job failed; open Activity for its log." });
    return null;
  };

  // busyWhile runs fn with the dialog locked.
  const busyWhile = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
    } finally {
      setBusy(false);
    }
  };

  const finish = (finished: Job | null, okText: string) => {
    void queryClient.invalidateQueries({ queryKey: keys.config });
    if (!finished) return;
    notify(finished.result?.changed === false ? "Already up to date." : okText);
    onClose();
  };

  const find = () => {
    const e: Errors = {
      repo: repoError(repo),
      path: pathError(path.trim()),
      ref: refError(ref.trim()),
    };
    if (e.repo || e.path || e.ref) {
      setErrors(e);
      return;
    }
    void busyWhile(async () => {
      const found = await run("skills.scan", {
        repo: normalizeRepo(repo),
        ref: ref.trim(),
        path: path.trim(),
      });
      const result = found ? scanOf(found) : null;
      if (!result) return;
      setScan(result);
      setPicks(defaultPicks(result.skills));
      setJob(null);
    });
  };

  const addPicked = () => {
    if (!scan) return;
    const paths = scan.skills.filter((r) => picks.includes(r.path)).map((r) => r.path);
    void busyWhile(async () => {
      const added = await run("skills.add", {
        repo: normalizeRepo(repo),
        ref: ref.trim(),
        commit: scan.commit,
        paths,
        agents: picked,
      });
      finish(added, `${count(paths.length, "skill", "skills")} added.${BG}`);
    });
  };

  const addFromCatalog = (c: CatalogEntry) =>
    void busyWhile(async () => {
      const found = await run("skills.scan", { repo: c.repo, ref: c.ref, path: c.path });
      const result = found ? scanOf(found) : null;
      if (!result) return;
      const row = result.skills.find((r) => r.path === c.path);
      if (!row || row.error || row.installed) {
        setErrors({
          form:
            row?.error ??
            (row?.installed
              ? `${c.id} is already installed.`
              : `${c.id} is no longer in that repository.`),
        });
        return;
      }
      const added = await run("skills.add", {
        repo: c.repo,
        ref: c.ref,
        commit: result.commit,
        paths: [c.path],
        agents: picked,
      });
      finish(added, `${c.id} added.${BG}`);
    });

  const saveEdit = () => {
    if (!editing) return;
    const r = ref.trim();
    const e = refError(r);
    if (e) {
      setErrors({ ref: e });
      return;
    }
    void busyWhile(async () => {
      if (JSON.stringify(picked) !== JSON.stringify(editing.skill.agents)) {
        try {
          await applyConfig(agentsPatch(editing.id, picked));
        } catch (err) {
          setErrors({ form: errorText(err) });
          return;
        }
      }
      if (r !== editing.skill.ref) {
        const targets = grouped && applyAll ? group.map((g) => g.id) : [editing.id];
        const moved = await run("skills.update", { ids: targets, ref: r });
        const who = targets.length > 1 ? count(targets.length, "skill", "skills") : editing.id;
        finish(moved, `${who} moved to ${r}.${BG}`);
        return;
      }
      void queryClient.invalidateQueries({ queryKey: keys.config });
      notify(`${editing.id} saved.${BG}`);
      onClose();
    });
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
            <Segmented
              label="Skill source"
              value={tab}
              onChange={setTab}
              className="w-full"
              options={[
                { value: "catalog", label: "Catalog" },
                { value: "repo", label: "From a repository" },
              ]}
            />
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
        {!editing && tab === "repo" && !scan ? (
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
              hint="A folder to look in; empty searches the whole repository."
              error={errors.path}
            >
              <Input
                {...fieldProps(ids.path, errors.path)}
                value={path}
                placeholder="skills"
                className="font-mono text-xs"
                spellCheck={false}
                autoComplete="off"
                onChange={(ev) => setPath(ev.target.value)}
              />
            </FormField>
          </>
        ) : null}
        {editing || (tab === "repo" && !scan) ? (
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
        {grouped ? (
          <Label className="flex items-center gap-2 font-normal leading-[inherit]">
            <Checkbox
              checked={applyAll}
              disabled={busy}
              onCheckedChange={(on) => setApplyAll(on)}
            />
            Apply to all {group.length} skills from {repoLabel(editing.skill.repo)}
          </Label>
        ) : null}
        {!editing && tab === "repo" && scan ? (
          <ScanChecklist
            rows={scan.skills}
            commit={scan.commit}
            picks={picks}
            disabled={busy}
            onChange={setPicks}
          />
        ) : null}
        {editing || tab === "catalog" || scan ? (
          <AgentChecklist
            value={picked}
            agents={agents}
            unsupported={unsupported}
            notes={viaNotes(picked, agents)}
            disabled={busy}
            onChange={setPicked}
          />
        ) : null}
        {errors.form ? <InlineAlert tone="bad">{errors.form}</InlineAlert> : null}
        {job?.log ? <LogViewer text={job.log} truncated={job.log_truncated} /> : null}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          {editing ? (
            <BusyButton busy={busy} busyLabel="Saving…" onClick={saveEdit}>
              Save
            </BusyButton>
          ) : tab === "repo" && !scan ? (
            <BusyButton busy={busy} busyLabel="Finding…" onClick={find}>
              Find skills
            </BusyButton>
          ) : tab === "repo" && scan ? (
            <>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  setScan(null);
                  setErrors({});
                }}
              >
                Back
              </Button>
              <BusyButton
                busy={busy}
                busyLabel="Adding…"
                disabled={picks.length === 0}
                onClick={addPicked}
              >
                Add {count(picks.length, "skill", "skills")}
              </BusyButton>
            </>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
