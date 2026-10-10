import { useEffect, useEffectEvent, useId, useRef, useState } from "react";
import { keys, queryClient } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { Job, NewSourceScan } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LogViewer } from "@/components/log-viewer";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { count } from "@/lib/format";
import { notify } from "@/lib/notify";
import { cn } from "@/lib/utils";
import { normalizeRepo, pathError, refError, repoError, shortCommit } from "./skills-logic";
import { addedText, newScanOf, sourceIdError, sourceResult } from "./sources-logic";

type Errors = Partial<Record<"repo" | "path" | "ref" | "id" | "form", string>>;

/** SourceStart prefills the dialog; find scans it as soon as it opens. */
export interface SourceStart {
  repo: string;
  ref: string;
  path: string;
  find?: boolean;
}

const mono = "font-mono text-xs";

/** SourceDialog adds a skill source: Find previews the repository folder,
 * then Add source saves that commit with every skill found, optionally
 * attaching them all to every agent. */
export function SourceDialog({
  start,
  onClose,
  onAdded,
}: {
  start?: SourceStart;
  onClose: () => void;
  onAdded: (id: string) => void;
}) {
  const [repo, setRepo] = useState(start?.repo ?? "");
  const [ref, setRef] = useState(start?.ref ?? "main");
  const [path, setPath] = useState(start?.path ?? "");
  const [scan, setScan] = useState<NewSourceScan | null>(null);
  const [id, setId] = useState("");
  const [attachAll, setAttachAll] = useState(false);
  const [errors, setErrors] = useState<Errors>({});
  const [job, setJob] = useState<Job | null>(null);
  const [busy, setBusy] = useState(false);
  const ids = { repo: useId(), ref: useId(), path: useId(), id: useId(), head: useId() };

  // run starts a job and returns it if it succeeded; a failure lands on its
  // field when that field is on screen, else under the form.
  const run = async (verb: string, body: object): Promise<Job | null> => {
    setErrors({});
    const finished = await runJob(verb, body, setJob);
    if (!finished || finished.state === "succeeded") return finished;
    const err = finished.result?.error;
    const shown = scan ? ["id"] : ["repo", "path", "ref"];
    const key = err?.field && shown.includes(err.field) ? (err.field as keyof Errors) : "form";
    setErrors({ [key]: err?.message ?? "The job failed; open Activity for its log." });
    return null;
  };

  const find = async () => {
    const e: Errors = {
      repo: repoError(repo),
      path: pathError(path.trim()),
      ref: refError(ref.trim()),
    };
    if (e.repo || e.path || e.ref) {
      setErrors(e);
      return;
    }
    setBusy(true);
    const found = await run("skills.sources.preview", {
      repo: normalizeRepo(repo),
      ref: ref.trim(),
      path: path.trim(),
    });
    const result = found ? newScanOf(found) : null;
    if (result) {
      setScan(result);
      setId(result.id);
      setJob(null);
    }
    setBusy(false);
  };

  const add = async () => {
    if (!scan) return;
    const e = sourceIdError(id.trim());
    if (e) {
      setErrors({ id: e });
      return;
    }
    setBusy(true);
    const added = await run("skills.sources.add", {
      repo: normalizeRepo(repo),
      ref: ref.trim(),
      path: path.trim(),
      id: id.trim(),
      commit: scan.commit,
      attach_all: attachAll,
    });
    setBusy(false);
    void queryClient.invalidateQueries({ queryKey: keys.config });
    if (!added) return;
    const r = sourceResult(added);
    notify(addedText(r));
    onAdded(r.id ?? id.trim());
  };

  // A prefilled source finds once, on open: the ref stops later renders
  // (and StrictMode's second run) from scanning again.
  const found = useRef(false);
  const findOnOpen = useEffectEvent(() => void find());
  useEffect(() => {
    if (!start?.find || found.current) return;
    found.current = true;
    findOnOpen();
  }, [start]);

  const rows = scan ? Object.entries(scan.found).sort(([a], [b]) => a.localeCompare(b)) : [];
  const valid = rows.filter(([, f]) => !f.error).length;

  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Add a skill source</DialogTitle>
        </DialogHeader>
        <p className="text-xs text-fg-muted">
          Skills are instructions and scripts your agents run. Add only sources you trust.
        </p>
        {scan ? (
          <>
            <div role="group" aria-labelledby={ids.head} className="flex flex-col gap-2">
              <p id={ids.head} className="font-medium">
                {count(valid, "skill", "skills")} at{" "}
                <code className={mono}>{shortCommit(scan.commit)}</code>
              </p>
              <ul aria-label="Skills found" className="flex max-h-64 flex-col overflow-y-auto">
                {rows.map(([name, f]) => (
                  <li
                    key={name}
                    className="flex flex-col gap-0.5 border-b border-line py-2 last:border-b-0"
                  >
                    <span className={cn(mono, "font-semibold text-fg")}>{name}</span>
                    <span className={cn("text-xs", f.error ? "text-warn" : "text-fg-muted")}>
                      {f.error ?? f.description}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
            {scan.exists ? (
              <InlineAlert tone="warn">
                Source {scan.exists} already tracks this repository at that ref and path.
              </InlineAlert>
            ) : null}
            <FormField
              id={ids.id}
              label="Id"
              hint="Berth's name for this source, and its folder in the skills store."
              error={errors.id}
            >
              <Input
                {...fieldProps(ids.id, errors.id)}
                value={id}
                className={mono}
                spellCheck={false}
                autoComplete="off"
                onChange={(ev) => setId(ev.target.value)}
              />
            </FormField>
            <Label className="flex items-center gap-2 leading-[inherit] font-normal">
              <Checkbox
                checked={attachAll}
                disabled={busy}
                onCheckedChange={(on) => setAttachAll(on)}
              />
              Attach all to all agents
            </Label>
          </>
        ) : (
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
                className={mono}
                spellCheck={false}
                autoComplete="off"
                onChange={(ev) => setRepo(ev.target.value)}
              />
            </FormField>
            <FormField
              id={ids.ref}
              label="Ref"
              hint="A branch, tag or commit. Berth pins it to a commit and moves only when you update."
              error={errors.ref}
            >
              <Input
                {...fieldProps(ids.ref, errors.ref)}
                value={ref}
                className={mono}
                spellCheck={false}
                autoComplete="off"
                onChange={(ev) => setRef(ev.target.value)}
              />
            </FormField>
            <FormField
              id={ids.path}
              label="Path"
              hint="The folder holding the skills; empty searches the whole repository."
              error={errors.path}
            >
              <Input
                {...fieldProps(ids.path, errors.path)}
                value={path}
                placeholder="skills"
                className={mono}
                spellCheck={false}
                autoComplete="off"
                onChange={(ev) => setPath(ev.target.value)}
              />
            </FormField>
          </>
        )}
        {errors.form ? <InlineAlert tone="bad">{errors.form}</InlineAlert> : null}
        {job?.log ? <LogViewer text={job.log} truncated={job.log_truncated} /> : null}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          {scan ? (
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
                disabled={Boolean(scan.exists)}
                onClick={() => void add()}
              >
                Add source
              </BusyButton>
            </>
          ) : (
            <BusyButton busy={busy} busyLabel="Finding…" onClick={() => void find()}>
              Find skills
            </BusyButton>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
