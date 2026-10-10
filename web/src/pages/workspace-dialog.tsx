import { useId, useState } from "react";
import { refreshWorkspaces, useCandidates } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { Config, Job } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
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
import { Input } from "@/components/ui/input";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { normalizeRepo, refError, repoError } from "./skills-logic";
import { defaultPath, deriveId, folderError, idError } from "./workspaces-logic";

type Tab = "clone" | "adopt";
type Field = "repo" | "ref" | "path" | "id";
type Errors = Partial<Record<Field | "form", string>>;

const FIELDS: readonly string[] = ["repo", "ref", "path", "id"];

/** AddWorkspaceDialog clones a repository into /workspaces or adopts a
 * folder already there. The folder follows the repository, and the id the
 * folder, until the user edits them (null means "follow"). */
export function AddWorkspaceDialog({
  config,
  onClose,
  onAdded,
}: {
  config: Config | undefined;
  onClose: () => void;
  onAdded: (id: string) => void;
}) {
  const [tab, setTab] = useState<Tab>("clone");
  const [repo, setRepo] = useState("");
  const [ref, setRef] = useState("");
  const [path, setPath] = useState<string | null>(null);
  const [id, setId] = useState<string | null>(null);
  const [picked, setPicked] = useState("");
  const [errors, setErrors] = useState<Errors>({});
  const [job, setJob] = useState<Job | null>(null);
  const [busy, setBusy] = useState(false);
  const candidates = useCandidates(tab === "adopt");
  const ids = { repo: useId(), ref: useId(), path: useId(), id: useId(), list: useId() };

  const folder = tab === "clone" ? (path ?? defaultPath(normalizeRepo(repo))) : picked;
  const wsId = id ?? deriveId(folder);
  const rows = candidates.data?.candidates ?? [];

  const submit = async () => {
    const e: Errors = {
      path: tab === "adopt" && !picked ? "Choose a folder." : folderError(folder, config),
      id: idError(wsId, config),
    };
    if (tab === "clone") {
      e.repo = repoError(repo);
      e.ref = ref.trim() ? refError(ref.trim()) : "";
    }
    if (Object.values(e).some(Boolean)) {
      setErrors(e);
      return;
    }
    setErrors({});
    setBusy(true);
    const body =
      tab === "clone"
        ? { mode: "clone", repo: normalizeRepo(repo), ref: ref.trim(), path: folder, id: wsId }
        : { mode: "adopt", path: folder, id: wsId };
    const finished = await runJob("workspaces.add", body, setJob);
    setBusy(false);
    void refreshWorkspaces();
    if (!finished) return;
    if (finished.state !== "succeeded") {
      const err = finished.result?.error;
      const key = err?.field && FIELDS.includes(err.field) ? (err.field as Field) : "form";
      setErrors({ [key]: err?.message ?? "The job failed; open Activity for its log." });
      return;
    }
    notify(`${wsId} ${tab === "clone" ? "cloned" : "added"}.`);
    onAdded(wsId);
  };

  const input = (
    key: Field,
    value: string,
    onChange: (v: string) => void,
    placeholder?: string,
  ) => (
    <Input
      {...fieldProps(ids[key], errors[key])}
      value={value}
      placeholder={placeholder}
      className="font-mono text-xs"
      spellCheck={false}
      autoComplete="off"
      disabled={busy}
      onChange={(ev) => onChange(ev.target.value)}
    />
  );

  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Add a workspace</DialogTitle>
        </DialogHeader>
        <Segmented
          label="How to add it"
          value={tab}
          className="w-full"
          onChange={(t) => {
            setTab(t);
            setErrors({});
            setId(null);
          }}
          options={[
            { value: "clone", label: "Clone a repository" },
            { value: "adopt", label: "Adopt a folder" },
          ]}
        />
        {tab === "clone" ? (
          <>
            <FormField
              id={ids.repo}
              label="Repository"
              hint="owner/repo for GitHub, or an https URL."
              error={errors.repo}
            >
              {input("repo", repo, setRepo)}
            </FormField>
            <FormField
              id={ids.ref}
              label="Ref"
              hint="A branch, tag or commit; empty clones the default branch."
              error={errors.ref}
            >
              {input("ref", ref, setRef, "main")}
            </FormField>
            <FormField
              id={ids.path}
              label="Folder"
              hint="Inside /workspaces. Berth creates it; it must not exist yet."
              error={errors.path}
            >
              {input("path", folder, setPath)}
            </FormField>
          </>
        ) : (
          <div className="flex flex-col gap-1.5">
            <span id={ids.list} className="font-medium">
              Folder
            </span>
            {candidates.error ? (
              <InlineAlert tone="bad">
                {errorText(candidates.error, "Could not list /workspaces.")}
              </InlineAlert>
            ) : candidates.isPending ? (
              <LoadingRows n={2} />
            ) : rows.length ? (
              <div
                role="radiogroup"
                aria-labelledby={ids.list}
                className="flex max-h-64 flex-col overflow-y-auto rounded-md border border-line-control"
              >
                {rows.map((c) => (
                  <label
                    key={c.path}
                    className="flex items-center gap-2.5 border-b px-3 py-2 last:border-b-0 hover:bg-surface-sunken"
                  >
                    <input
                      type="radio"
                      name={ids.list}
                      value={c.path}
                      checked={picked === c.path}
                      disabled={busy}
                      className="accent-primary"
                      onChange={() => {
                        setPicked(c.path);
                        setId(null);
                      }}
                    />
                    <span className="min-w-0 truncate font-mono text-xs">{c.path}</span>
                    {c.git ? <span className="ml-auto text-xs text-fg-muted">git</span> : null}
                  </label>
                ))}
              </div>
            ) : (
              <p className="text-fg-muted">
                No folders to adopt. Create one in /workspaces, or clone a repository instead.
              </p>
            )}
            {errors.path ? <p className="text-xs text-bad">{errors.path}</p> : null}
          </div>
        )}
        <FormField
          id={ids.id}
          label="Id"
          hint={`Names the workspace and its terminal session, ws-${wsId || "…"}.`}
          error={errors.id}
        >
          {input("id", wsId, setId)}
        </FormField>
        {errors.form ? <InlineAlert tone="bad">{errors.form}</InlineAlert> : null}
        {job?.log ? <LogViewer text={job.log} truncated={job.log_truncated} /> : null}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <BusyButton
            busy={busy}
            busyLabel={tab === "clone" ? "Cloning…" : "Adding…"}
            onClick={() => void submit()}
          >
            {tab === "clone" ? "Clone" : "Add workspace"}
          </BusyButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
