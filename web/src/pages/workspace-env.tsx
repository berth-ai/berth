import { useId, useState } from "react";
import { applyConfig } from "@/api/config";
import { keys, queryClient, useConfig } from "@/api/queries";
import { restartSession } from "@/api/workspaces";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { applyAndNotify } from "./apply-notify";
import {
  type EnvRow,
  envKeyError,
  envRows,
  envValueError,
  removePatch,
  secretName,
  setPlainPatch,
  setSecretPatch,
} from "./env-logic";

/** WorkspaceEnv is a workspace's Env tab (DESIGN.md §4). */
export function WorkspaceEnv({ id }: { id: string }) {
  const config = useConfig();
  const [confirmDialog, confirm] = useConfirm();
  const [editing, setEditing] = useState<EnvRow | "new" | null>(null);
  const view = config.data;
  const ws = view?.config.workspaces?.[id];
  if (!view || !ws) return <LoadingRows n={2} />;
  const rows = envRows(id, ws, view.secrets ?? {});
  const clearSecret = (r: EnvRow) =>
    confirm({
      title: `Clear ${r.key}?`,
      body: `New windows in ${id} start without ${r.key} until you set it again.`,
      confirm: "Clear",
      danger: true,
      action: async () => {
        try {
          await applyConfig(null, { [secretName(id, r.key)]: null });
        } catch (err) {
          notify(errorText(err), "bad");
          return false;
        }
        notify(`${r.key} cleared.`);
        await queryClient.invalidateQueries({ queryKey: keys.config });
      },
    });
  const remove = (r: EnvRow) =>
    confirm({
      title: `Remove ${r.key}?`,
      body: r.secret ? "Berth deletes its value too." : `New windows in ${id} start without it.`,
      confirm: "Remove",
      danger: true,
      action: () => applyAndNotify(removePatch(id, r.key), `${r.key} removed from ${id}.`),
    });

  return (
    <>
      <InlineAlert tone="info">
        <div className="flex flex-wrap items-center gap-2">
          <span>
            Variables reach windows opened after a change. Restart the session to apply them
            everywhere.
          </span>
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              confirm({
                title: `Restart ws-${id}?`,
                body: "Berth closes every window of the session, including running agents, and opens one shell.",
                confirm: "Restart session",
                danger: true,
                action: async () => {
                  if (!(await restartSession(id))) return false;
                  notify(`ws-${id} restarted.`);
                },
              })
            }
          >
            Restart session
          </Button>
        </div>
      </InlineAlert>
      <PanelCard
        title="Variables"
        actions={
          <Button size="sm" onClick={() => setEditing("new")}>
            Add variable
          </Button>
        }
      >
        {rows.length ? (
          <Table aria-label={`Variables of ${id}`}>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Value</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((r) => (
                <TableRow key={r.key}>
                  <TableCell className="font-mono text-xs font-semibold">{r.key}</TableCell>
                  <TableCell>
                    {r.secret ? (
                      <StatusBadge state={r.set ? "set" : "not_set"} />
                    ) : (
                      <span className="line-clamp-2 font-mono text-xs break-all whitespace-pre-wrap">
                        {r.value}
                      </span>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1.5">
                      <Button
                        size="sm"
                        variant="outline"
                        aria-label={`${r.secret ? "Replace" : "Edit"} ${r.key}`}
                        onClick={() => setEditing(r)}
                      >
                        {r.secret ? "Replace" : "Edit"}
                      </Button>
                      {r.secret && r.set ? (
                        <Button
                          size="sm"
                          variant="outline"
                          aria-label={`Clear ${r.key}`}
                          onClick={() => clearSecret(r)}
                        >
                          Clear
                        </Button>
                      ) : null}
                      <Button
                        size="sm"
                        variant="outline"
                        aria-label={`Remove ${r.key}`}
                        onClick={() => remove(r)}
                      >
                        Remove
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyState
            text={`No variables yet. Windows in ${id} get the container's environment.`}
          />
        )}
      </PanelCard>
      <p className="text-xs text-fg-muted">
        Secret values are stored outside the config and never shown again. Code-server terminals and
        docker exec shells do not get workspace variables.
      </p>
      {editing ? (
        <EnvDialog
          id={id}
          existing={rows.map((r) => r.key)}
          edit={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
        />
      ) : null}
      {confirmDialog}
    </>
  );
}

/** EnvDialog adds a variable, edits a plain value, or replaces a secret. */
export function EnvDialog({
  id,
  existing,
  edit,
  onClose,
}: {
  id: string;
  existing: string[];
  edit?: EnvRow;
  onClose: () => void;
}) {
  const nameId = useId();
  const valueId = useId();
  const [key, setKey] = useState(edit?.key ?? "");
  const [value, setValue] = useState(edit && !edit.secret ? edit.value : "");
  const [secret, setSecret] = useState(edit?.secret ?? false);
  const [busy, setBusy] = useState(false);
  const keyError = edit ? "" : key ? envKeyError(key, existing) : "";
  const valueError = envValueError(value) || (secret && !value ? "Enter the secret's value." : "");
  const label = !edit ? "Add variable" : edit.secret ? "Replace" : "Save";

  const save = async () => {
    setBusy(true);
    let ok = true;
    if (secret) {
      try {
        await applyConfig(edit ? null : setSecretPatch(id, key), { [secretName(id, key)]: value });
        notify(`${key} is set in ${id}; new windows get it.`);
      } catch (err) {
        notify(errorText(err), "bad");
        ok = false;
      }
      await queryClient.invalidateQueries({ queryKey: keys.config });
    } else {
      ok = await applyAndNotify(
        setPlainPatch(id, key, value),
        `${key} is set in ${id}; new windows get it.`,
      );
    }
    setBusy(false);
    if (ok) onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {edit ? `${edit.secret ? "Replace" : "Edit"} ${edit.key}` : `Add a variable to ${id}`}
          </DialogTitle>
        </DialogHeader>
        <FormField id={nameId} label="Name" error={keyError}>
          <Input
            {...fieldProps(nameId, keyError)}
            value={key}
            disabled={Boolean(edit)}
            className="font-mono text-xs"
            spellCheck={false}
            autoComplete="off"
            onChange={(e) => setKey(e.target.value.toUpperCase())}
          />
        </FormField>
        {!edit ? (
          <div className="flex flex-col gap-1">
            <Label className="flex items-center gap-2 leading-[inherit] font-normal">
              <Checkbox checked={secret} disabled={busy} onCheckedChange={(on) => setSecret(on)} />
              Secret
            </Label>
            <p className="text-xs text-fg-muted">
              Stored outside the config and never shown again.
            </p>
          </div>
        ) : null}
        <FormField id={valueId} label="Value" error={value ? valueError : ""}>
          <Input
            {...fieldProps(valueId, value ? valueError : "")}
            type={secret ? "password" : "text"}
            value={value}
            className="font-mono text-xs"
            spellCheck={false}
            autoComplete="off"
            onChange={(e) => setValue(e.target.value)}
          />
        </FormField>
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <BusyButton
            busy={busy}
            busyLabel="Saving…"
            disabled={!key || Boolean(keyError) || Boolean(valueError)}
            onClick={() => void save()}
          >
            {label}
          </BusyButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
