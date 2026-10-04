import { useId, useState } from "react";
import { applyConfig } from "@/api/config";
import { refreshLogins, refreshStatus } from "@/api/queries";
import type { AgentPackage, LoginMethodStatus, LoginStatus } from "@/api/types";
import { BrandIcon } from "@/components/brand-icon";
import { EmptyState } from "@/components/empty-state";
import { LoadingRows } from "@/components/loading";
import { OnOffSwitch } from "@/components/on-off-switch";
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
import { disabledAfter, keyBadge } from "./agents-logic";
import { SignInCell } from "./sign-in";

/** SignInColumn feeds the agents table's Sign-in column. */
export interface SignInColumn {
  data: LoginStatus | undefined;
  loading: boolean;
  failed: boolean;
  onSignIn: (p: AgentPackage, m: LoginMethodStatus) => void;
  onSignOut: (p: AgentPackage, m: LoginMethodStatus) => void;
}

/** PackageSection lists the lock packages of one kind: a switch each, key
 * status, and key editors that expand in place. */
export function PackageSection({
  title,
  kind,
  nameHeader,
  noun,
  all,
  empty,
  signIn,
}: {
  title: string;
  kind: "agent" | "mcp";
  nameHeader: string;
  noun: [string, string];
  all: AgentPackage[] | undefined;
  empty: string;
  signIn?: SignInColumn;
}) {
  const list = all?.filter((p) => p.kind === kind);
  return (
    <PanelCard
      title={title}
      badge={
        list ? <span className="text-xs text-fg-muted">{count(list.length, ...noun)}</span> : null
      }
    >
      {!all || !list ? (
        <LoadingRows n={2} />
      ) : list.length ? (
        <Table aria-label={title}>
          <TableHeader>
            <TableRow>
              <TableHead>{nameHeader}</TableHead>
              <TableHead>Keys</TableHead>
              {signIn ? <TableHead>Sign-in</TableHead> : null}
              <TableHead>Enabled</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.map((p) => (
              <PackageRows key={p.package} p={p} all={all} signIn={signIn} />
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState text={empty} />
      )}
    </PanelCard>
  );
}

function PackageRows({
  p,
  all,
  signIn,
}: {
  p: AgentPackage;
  all: AgentPackage[];
  signIn?: SignInColumn;
}) {
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState<boolean | null>(null);
  const detailId = useId();
  const keys = p.keys ?? [];

  const toggle = async (on: boolean) => {
    setPending(on);
    try {
      await applyConfig({ agents: { disabled: disabledAfter(all, p.package, on) } });
      notify(`${p.name}${on ? " is on." : " is off."} MCP registrations update in the background.`);
      await refreshStatus();
      void refreshLogins();
    } catch (err) {
      notify(errorText(err), "bad");
    }
    setPending(null);
  };

  const saveKey = (name: string, value: string | null, done: string) => async () => {
    try {
      await applyConfig(null, { [name]: value });
    } catch (err) {
      return fieldErrors(err)[`secrets.${name}`] || errorText(err);
    }
    notify(done);
    void refreshStatus();
    return null;
  };

  return (
    <>
      <TableRow>
        <TableCell data-label="">
          <div className="flex min-w-0 items-center gap-2.5">
            {p.kind === "agent" ? <BrandIcon slug={p.icon} name={p.name} id={p.package} /> : null}
            <div className="flex min-w-0 flex-col gap-0.5">
              <strong className="text-fg">{p.name}</strong>
              <span className="font-mono text-xs break-all text-fg-muted">
                {p.package} {p.version}
              </span>
            </div>
          </div>
        </TableCell>
        <TableCell data-label="Keys">
          {keys.length ? (
            <div className="flex flex-col gap-1">
              {keys.map((k) => (
                <span key={k.name} className="inline-flex flex-wrap items-center gap-2">
                  <span className="font-mono text-xs">{k.name}</span>
                  <StatusBadge small state={keyBadge(k)} />
                </span>
              ))}
            </div>
          ) : (
            <span className="text-xs text-fg-muted">None needed</span>
          )}
        </TableCell>
        {signIn ? (
          <TableCell data-label="Sign-in">
            <SignInCell
              pkg={p}
              status={signIn.data?.agents?.[p.package] ?? undefined}
              loading={signIn.loading}
              failed={signIn.failed}
              onSignIn={(m) => signIn.onSignIn(p, m)}
              onSignOut={(m) => signIn.onSignOut(p, m)}
            />
          </TableCell>
        ) : null}
        <TableCell data-label="Enabled">
          <OnOffSwitch
            checked={pending ?? p.enabled}
            disabled={pending !== null}
            label={`${p.enabled ? "Turn off" : "Turn on"} ${p.name}`}
            onCheckedChange={(on) => void toggle(on)}
          />
        </TableCell>
        <TableCell className="text-right">
          {keys.length ? (
            <Button
              variant="outline"
              size="sm"
              aria-expanded={open}
              aria-controls={open ? detailId : undefined}
              aria-label={`Manage ${p.name}`}
              onClick={() => setOpen((o) => !o)}
            >
              Manage
            </Button>
          ) : null}
        </TableCell>
      </TableRow>
      {open ? (
        <TableRow id={detailId} className="hover:bg-transparent">
          <TableCell colSpan={signIn ? 5 : 4}>
            <div className="grid grid-cols-[repeat(auto-fill,minmax(min(280px,100%),1fr))] gap-3 py-1">
              {keys.map((k) => (
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
