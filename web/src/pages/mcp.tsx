import { Plus } from "lucide-react";
import { useId, useState } from "react";
import { applyConfig } from "@/api/config";
import { keys, queryClient, refreshStatus, useConfig, useStatus } from "@/api/queries";
import type { AgentPackage } from "@/api/types";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { LoadingRows } from "@/components/loading";
import { OnOffSwitch } from "@/components/on-off-switch";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { SecretField } from "@/components/secret-field";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorText, fieldErrors } from "@/lib/errors";
import { count } from "@/lib/format";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { disabledAfter, keyBadge } from "./agents-logic";
import { AvailablePicker } from "./available-picker";
import {
  AUTH_LABELS,
  availablePatch,
  enabledPatch,
  type McpRow,
  mcpRows,
  removePatch,
  tokenName,
} from "./mcp-logic";
import { type Editing, McpServerDialog } from "./mcp-server-dialog";

const BG = " MCP registrations update in the background.";

// MCP servers: the lock's servers and the user's remote ones, who gets each, on or off.
export function McpPage() {
  useTitle("MCP servers");
  const status = useStatus();
  const config = useConfig();
  const [dialog, setDialog] = useState<Editing | "add" | null>(null);
  const [confirmDialog, confirm] = useConfirm();
  const packages = status.data ? (status.data.packages ?? []) : undefined;
  const view = config.data;
  const rows = packages && view ? mcpRows(packages, view.config, view.secrets ?? {}) : undefined;
  const agents = (packages ?? []).filter((p) => p.kind === "agent");
  const available = view?.config.mcp?.available ?? {};

  const apply = async (patch: object, done: string) => {
    try {
      await applyConfig(patch);
      notify(done + BG);
    } catch (err) {
      notify(errorText(err), "bad");
    }
    await Promise.all([queryClient.invalidateQueries({ queryKey: keys.config }), refreshStatus()]);
  };

  return (
    <Page>
      <PageHeader
        title="MCP servers"
        description="Tools your agents can call. Choose which agents get each server."
      />
      <PanelCard
        title="Servers"
        badge={
          rows ? (
            <span className="text-xs text-fg-muted">{count(rows.length, "server", "servers")}</span>
          ) : null
        }
        actions={
          <Button size="sm" onClick={() => setDialog("add")}>
            <Plus aria-hidden /> Add server
          </Button>
        }
      >
        {!rows ? (
          <LoadingRows n={2} />
        ) : rows.length ? (
          <Table aria-label="Servers">
            <TableHeader>
              <TableRow>
                <TableHead>Server</TableHead>
                <TableHead>Transport</TableHead>
                <TableHead>Auth</TableHead>
                <TableHead>Available to</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((r) => (
                <ServerRows
                  key={`${r.kind}:${r.id || r.name}`}
                  row={r}
                  agents={agents}
                  all={packages ?? []}
                  available={r.id ? available[r.id] : undefined}
                  onApply={apply}
                  onEdit={setDialog}
                  onRemove={(id) =>
                    confirm({
                      title: `Remove ${id}?`,
                      body: "Berth removes it from every agent's configuration and deletes its token.",
                      confirm: "Remove",
                      danger: true,
                      action: () => apply(removePatch(id), `${id} removed.`),
                    })
                  }
                />
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyState text="No MCP servers yet." />
        )}
      </PanelCard>
      {dialog ? (
        <McpServerDialog
          editing={dialog === "add" ? null : dialog}
          taken={(rows ?? []).map((r) => r.id).filter(Boolean)}
          agents={agents}
          onClose={() => setDialog(null)}
        />
      ) : null}
      {confirmDialog}
    </Page>
  );
}

function ServerRows({
  row,
  agents,
  all,
  available,
  onApply,
  onEdit,
  onRemove,
}: {
  row: McpRow;
  agents: AgentPackage[];
  all: AgentPackage[];
  available: string[] | undefined;
  onApply: (patch: object, done: string) => Promise<void>;
  onEdit: (e: Editing) => void;
  onRemove: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState<boolean | null>(null);
  const detailId = useId();
  const isLock = row.kind === "lock";
  const enabled = isLock ? row.pkg.enabled : row.server.enabled;
  const bearer = !isLock && row.server.auth === "bearer";
  const secrets = isLock
    ? (row.pkg.keys ?? [])
    : bearer
      ? [{ name: tokenName(row.id), set: row.tokenSet }]
      : [];

  const toggle = async (on: boolean) => {
    setPending(on);
    const patch = isLock
      ? { agents: { disabled: disabledAfter(all, row.pkg.package, on) } }
      : enabledPatch(row.id, on);
    await onApply(patch, `${row.name}${on ? " is on." : " is off."}`);
    setPending(null);
  };

  const saveKey = (name: string, value: string | null, done: string) => async () => {
    try {
      await applyConfig(null, { [name]: value });
    } catch (err) {
      return fieldErrors(err)[`secrets.${name}`] || errorText(err);
    }
    notify(done + (bearer ? BG : ""));
    void queryClient.invalidateQueries({ queryKey: keys.config });
    void refreshStatus();
    return null;
  };

  return (
    <>
      <TableRow>
        <TableCell data-label="">
          <div className="flex min-w-0 flex-col gap-0.5">
            <strong className="text-fg">{row.name}</strong>
            <span
              className="font-mono text-xs break-all text-fg-muted"
              title={isLock ? undefined : row.server.url}
            >
              {isLock ? `${row.pkg.package} ${row.pkg.version}` : row.server.url}
            </span>
            {!isLock && row.clash ? (
              <span className="w-fit">
                <StatusBadge small state="clash" />
              </span>
            ) : null}
          </div>
        </TableCell>
        <TableCell data-label="Transport">
          <span className="font-mono text-xs">{isLock ? "stdio" : "HTTP"}</span>
        </TableCell>
        <TableCell data-label="Auth">
          {isLock ? (
            secrets.length ? (
              <div className="flex flex-col gap-1">
                {secrets.map((k) => (
                  <span key={k.name} className="inline-flex flex-wrap items-center gap-2">
                    <span className="font-mono text-xs">{k.name}</span>
                    <StatusBadge small state={keyBadge(k)} />
                  </span>
                ))}
              </div>
            ) : (
              <span className="text-xs text-fg-muted">None needed</span>
            )
          ) : (
            <div className="flex flex-col gap-1 text-xs">
              <span className="inline-flex flex-wrap items-center gap-2">
                {AUTH_LABELS[row.server.auth]}
                {bearer ? <StatusBadge small state={row.tokenSet ? "set" : "not_set"} /> : null}
              </span>
              {row.server.auth === "oauth" ? (
                <span className="text-fg-muted">
                  Sign in from each agent (for example <code>/mcp</code> in Claude Code).
                </span>
              ) : null}
            </div>
          )}
        </TableCell>
        <TableCell data-label="Available to">
          {row.id ? (
            <AvailablePicker
              server={row.name}
              value={available}
              agents={agents}
              onChange={(next) =>
                onApply(
                  availablePatch(row.id, next),
                  `${row.name} is available to ${next ? "the chosen agents" : "all agents"}.`,
                )
              }
            />
          ) : (
            <span className="text-xs text-fg-muted">All agents</span>
          )}
        </TableCell>
        <TableCell data-label="Enabled">
          <OnOffSwitch
            checked={pending ?? enabled}
            disabled={pending !== null}
            label={`${enabled ? "Turn off" : "Turn on"} ${row.name}`}
            onCheckedChange={(on) => void toggle(on)}
          />
        </TableCell>
        <TableCell className="text-right">
          <div className="inline-flex flex-wrap justify-end gap-2 sm:flex-nowrap">
            {secrets.length ? (
              <Button
                variant="outline"
                size="sm"
                aria-expanded={open}
                aria-controls={open ? detailId : undefined}
                aria-label={`Manage ${row.name}`}
                onClick={() => setOpen((o) => !o)}
              >
                Manage
              </Button>
            ) : null}
            {isLock ? null : (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Edit ${row.id}`}
                  onClick={() => onEdit({ id: row.id, server: row.server, available })}
                >
                  Edit
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Remove ${row.id}`}
                  onClick={() => onRemove(row.id)}
                >
                  Remove
                </Button>
              </>
            )}
          </div>
        </TableCell>
      </TableRow>
      {open ? (
        <TableRow id={detailId} className="hover:bg-transparent">
          <TableCell colSpan={6}>
            <div className="grid grid-cols-[repeat(auto-fill,minmax(min(280px,100%),1fr))] gap-3 py-1">
              {secrets.map((k) => (
                <SecretField
                  key={k.name}
                  name={k.name}
                  isSet={k.set}
                  onSave={(value) => saveKey(k.name, value, `${k.name} saved.`)()}
                  onRemove={saveKey(k.name, null, `${k.name} removed.`)}
                />
              ))}
            </div>
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}
