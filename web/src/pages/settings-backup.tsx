import { useRef, useState, type ChangeEvent } from "react";
import { verb } from "@/api/request";
import type { ApplyResult, Config } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { PanelCard } from "@/components/panel-card";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { errorText } from "@/lib/errors";
import { describeChanges } from "@/lib/format";
import { notify } from "@/lib/notify";
import { exportedConfig, readImport, type ImportRow } from "./settings-logic";

function download(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "application/json" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** BackupCard exports, imports and reverts the config. */
export function BackupCard({ base, onReload }: { base: Config; onReload: () => void }) {
  const [dialog, confirm] = useConfirm();
  const [pending, setPending] = useState<{
    name: string;
    patch: Record<string, unknown>;
    rows: ImportRow[];
  } | null>(null);
  const [importing, setImporting] = useState(false);
  const file = useRef<HTMLInputElement>(null);

  const exportConfig = async () => {
    try {
      const r = await verb<{ config: Config }>("config.export");
      download(`berth-config-r${r.config.revision}.json`, `${JSON.stringify(r.config, null, 2)}\n`);
    } catch (err) {
      notify(errorText(err), "bad");
    }
  };

  const pick = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    e.target.value = "";
    if (!f) return;
    const found = exportedConfig(await f.text());
    if (!found.ok) {
      notify(found.error, "bad");
      return;
    }
    let upgraded: Config;
    try {
      // The broker turns older exports into the current shape (C2's skills into sources).
      upgraded = (await verb<{ config: Config }>("config.upgrade", { config: found.cfg })).config;
    } catch (err) {
      notify(errorText(err), "bad");
      return;
    }
    const parsed = readImport(JSON.stringify(upgraded), base);
    if (!parsed.ok) {
      notify(parsed.error, "bad");
      return;
    }
    setPending({ name: f.name, patch: parsed.patch, rows: parsed.rows });
  };

  const doImport = async () => {
    if (!pending) return;
    if (!pending.rows.length) {
      setPending(null);
      return;
    }
    setImporting(true);
    try {
      const r = await verb<ApplyResult>("config.apply", {
        base_revision: base.revision,
        patch: pending.patch,
      });
      setPending(null);
      notify(describeChanges(r.changes));
      onReload();
    } catch (err) {
      notify(errorText(err), "bad");
    }
    setImporting(false);
  };

  const revert = () =>
    confirm({
      title: "Revert to the previous settings?",
      body: "Berth applies the revision before the current one and restarts what it affects.",
      confirm: "Revert",
      action: async () => {
        try {
          const r = await verb<ApplyResult>("config.revert");
          notify(describeChanges(r.changes));
          onReload();
          return true;
        } catch (err) {
          notify(errorText(err), "bad");
          return false;
        }
      },
    });

  return (
    <PanelCard
      title="Backup"
      footer={
        <>
          <Button variant="outline" onClick={() => void exportConfig()}>
            Export config
          </Button>
          <Button variant="outline" onClick={() => file.current?.click()}>
            Import config
          </Button>
          <input
            ref={file}
            type="file"
            accept="application/json,.json"
            hidden
            onChange={(e) => void pick(e)}
          />
          <Button variant="outline" onClick={revert}>
            Revert to previous revision
          </Button>
        </>
      }
    >
      <p className="text-fg-muted">Exports leave out API keys and the login.</p>
      <Dialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open && !importing) setPending(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Import {pending?.name}?</DialogTitle>
          </DialogHeader>
          {pending?.rows.length ? (
            <>
              <p>These settings change:</p>
              <ul className="list-disc pl-5">
                {pending.rows.map((r) => (
                  <li key={r.path}>
                    <span className="font-mono text-xs">{r.path}</span>: {r.from} → {r.to}
                  </li>
                ))}
              </ul>
              <p className="text-fg-muted">
                API keys are never part of an export, so yours stay as they are.
              </p>
            </>
          ) : (
            <p>This file matches the current settings; nothing would change.</p>
          )}
          <DialogFooter>
            <Button variant="outline" disabled={importing} onClick={() => setPending(null)}>
              Cancel
            </Button>
            <BusyButton busy={importing} busyLabel="Importing…" onClick={() => void doImport()}>
              Import
            </BusyButton>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {dialog}
    </PanelCard>
  );
}
