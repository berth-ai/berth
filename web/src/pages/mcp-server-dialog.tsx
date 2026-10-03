import { useId, useState } from "react";
import { applyConfig } from "@/api/config";
import { keys, queryClient } from "@/api/queries";
import type { AgentPackage, McpAuth, McpServer } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { errorText, fieldErrors } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { AgentChecklist } from "./available-picker";
import { AUTH_LABELS, idError, serverPatch, tokenName, urlError } from "./mcp-logic";

export interface Editing {
  id: string;
  server: McpServer;
  available: string[] | undefined;
}

const AUTH_ITEMS = (Object.keys(AUTH_LABELS) as McpAuth[]).map((value) => ({
  value,
  label: AUTH_LABELS[value],
}));

/** McpServerDialog adds a remote server, or edits one when editing is set. */
export function McpServerDialog({
  editing,
  taken,
  agents,
  onClose,
}: {
  editing: Editing | null;
  taken: string[];
  agents: AgentPackage[];
  onClose: () => void;
}) {
  const [id, setId] = useState(editing?.id ?? "");
  const [url, setUrl] = useState(editing?.server.url ?? "");
  const [auth, setAuth] = useState<McpAuth>(editing?.server.auth ?? "none");
  const [token, setToken] = useState("");
  const [available, setAvailable] = useState<string[] | null>(editing?.available ?? null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const ids = { id: useId(), url: useId(), auth: useId(), token: useId() };
  const verb = editing ? "Save" : "Add server";

  const submit = async () => {
    const e: Record<string, string> = {
      id: editing ? "" : idError(id, taken),
      url: urlError(url),
    };
    setErrors(e);
    if (e.id || e.url) return;
    const sid = editing?.id ?? id;
    const server: McpServer = { url: url.trim(), auth, enabled: editing?.server.enabled ?? true };
    const secrets = auth === "bearer" && token ? { [tokenName(sid)]: token } : undefined;
    setBusy(true);
    try {
      await applyConfig(serverPatch(sid, server, available), secrets);
    } catch (err) {
      const f = fieldErrors(err);
      const p = `mcp.servers.${sid}`;
      const mapped = {
        id: f[p] ?? "",
        url: f[`${p}.url`] ?? "",
        token: f[`secrets.${tokenName(sid)}`] ?? "",
      };
      setErrors({ ...mapped, form: Object.values(mapped).some(Boolean) ? "" : errorText(err) });
      setBusy(false);
      return;
    }
    notify(`${sid} ${editing ? "saved" : "added"}. MCP registrations update in the background.`);
    await queryClient.invalidateQueries({ queryKey: keys.config });
    onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => (open || busy ? undefined : onClose())}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${editing.id}` : "Add an MCP server"}</DialogTitle>
        </DialogHeader>
        <FormField
          id={ids.id}
          label="Id"
          hint="The name agents see: lowercase letters, digits and dashes."
          error={errors.id}
        >
          <Input
            {...fieldProps(ids.id, errors.id)}
            value={id}
            disabled={!!editing}
            className="font-mono text-xs"
            spellCheck={false}
            autoComplete="off"
            onChange={(ev) => setId(ev.target.value.trim())}
          />
        </FormField>
        <FormField id={ids.url} label="URL" hint="A streamable HTTP endpoint." error={errors.url}>
          <Input
            {...fieldProps(ids.url, errors.url)}
            value={url}
            type="url"
            className="font-mono text-xs"
            spellCheck={false}
            autoComplete="off"
            onChange={(ev) => setUrl(ev.target.value)}
          />
        </FormField>
        <FormField
          id={ids.auth}
          label="Auth"
          hint={
            auth === "oauth"
              ? "Sign in from each agent, for example /mcp in Claude Code."
              : undefined
          }
        >
          <Select items={AUTH_ITEMS} value={auth} onValueChange={(v) => setAuth(v as McpAuth)}>
            <SelectTrigger {...fieldProps(ids.auth)} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {AUTH_ITEMS.map((i) => (
                <SelectItem key={i.value} value={i.value}>
                  {i.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FormField>
        {auth === "bearer" ? (
          <FormField
            id={ids.token}
            label="Token"
            error={errors.token}
            hint={
              editing?.server.auth === "bearer"
                ? "Leave empty to keep the saved token."
                : "Sent as Authorization: Bearer <token>."
            }
          >
            <Input
              {...fieldProps(ids.token, errors.token)}
              type="password"
              value={token}
              autoComplete="off"
              onChange={(ev) => setToken(ev.target.value.trim())}
            />
          </FormField>
        ) : null}
        <AgentChecklist value={available} agents={agents} onChange={setAvailable} />
        {errors.form ? <InlineAlert tone="bad">{errors.form}</InlineAlert> : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <BusyButton busy={busy} busyLabel={`${verb}…`} onClick={() => void submit()}>
            {verb}
          </BusyButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
