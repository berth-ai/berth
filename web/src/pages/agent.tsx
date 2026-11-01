import { Link, useNavigate } from "@tanstack/react-router";
import { type ReactNode, useState } from "react";
import { applyConfig } from "@/api/config";
import {
  refreshAgentRegistry,
  refreshLogins,
  refreshStatus,
  useLogins,
  useStatus,
  useUsage,
} from "@/api/queries";
import { verb } from "@/api/request";
import type { AgentPackage, AgentUsage, LoginMethodStatus } from "@/api/types";
import { BrandIcon } from "@/components/brand-icon";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { Facts, Mono } from "@/components/facts";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { OnOffSwitch } from "@/components/on-off-switch";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { SecretField } from "@/components/secret-field";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { UsageBars } from "@/components/usage-bars";
import { errorText, fieldErrors } from "@/lib/errors";
import { tokens } from "@/lib/format";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { startJob } from "./agents";
import { agentHome, disabledAfter, findAgent, lastActive, removeBody } from "./agents-logic";
import { SignInCell, useSignIn } from "./sign-in";

/** AgentPage is one agent: its usage, then its sign-in, keys and
 * settings, one section each. */
export function AgentPage({ slug }: { slug: string }) {
  const status = useStatus();
  const all = status.data?.packages ?? [];
  const p = findAgent(all, slug);
  useTitle(p?.name ?? "Agent");

  if (!status.data)
    return (
      <Page>
        <LoadingRows />
      </Page>
    );
  if (!p)
    return (
      <Page>
        <PageHeader title="Agent not found" />
        <PanelCard>
          <EmptyState
            text={`There is no agent named ${slug}.`}
            action={
              <Link to="/agents" className={buttonVariants({ variant: "outline" })}>
                Open Agents
              </Link>
            }
          />
        </PanelCard>
      </Page>
    );
  return <AgentSections p={p} all={all} />;
}

function AgentSections({ p, all }: { p: AgentPackage; all: AgentPackage[] }) {
  const logins = useLogins();
  const usage = useUsage();
  const signIn = useSignIn(logins.data);
  const [confirmDialog, ask] = useConfirm();
  const navigate = useNavigate();
  const methods = logins.data?.agents?.[p.package]?.methods ?? [];

  const signOut = (m: LoginMethodStatus) =>
    ask({
      title: `Sign ${p.name} out of ${m.label}?`,
      body: "The agent loses access until you sign in again.",
      confirm: "Sign out",
      danger: true,
      action: async () => {
        try {
          await verb("login.logout", { agent: p.package, method: m.id });
          notify(`${p.name} is signed out of ${m.label}.`);
          void refreshLogins();
        } catch (err) {
          notify(errorText(err), "bad");
          return false;
        }
      },
    });
  const remove = () =>
    ask({
      title: `Remove ${p.name}?`,
      body: removeBody(p),
      confirm: "Remove",
      danger: true,
      action: async () => {
        try {
          await verb("agents.remove", { package: p.package });
        } catch (err) {
          notify(errorText(err), "bad");
          return false;
        }
        notify(`${p.name} removed.`);
        void navigate({ to: "/agents" });
        void refreshStatus();
        void refreshLogins();
        void refreshAgentRegistry();
      },
    });
  const rollBack = () =>
    ask({
      title: `Roll ${p.name} back to ${p.previous}?`,
      body: "Berth switches back at once. It stays on that version until the image pins a newer one.",
      confirm: "Roll back",
      danger: true,
      action: startJob("agents.rollback", `${p.name} rolled back.`, { package: p.package }),
    });

  return (
    <Page>
      <header className="flex flex-wrap items-center gap-3">
        <BrandIcon slug={p.icon} name={p.name} id={p.package} />
        <div className="flex min-w-0 flex-[1_1_280px] flex-col gap-0.5">
          <h1>{p.name}</h1>
          <p className="flex flex-wrap items-center gap-2 font-mono text-xs break-all text-fg-muted">
            {p.package} {p.version}
          </p>
        </div>
      </header>
      {p.error ? (
        <InlineAlert tone="warn">
          {p.error}{" "}
          <a href="#/activity" className="underline">
            Open the job log
          </a>
        </InlineAlert>
      ) : null}
      {!p.enabled ? (
        <InlineAlert tone="info">
          {p.name} is off. Turn it on in Settings to sign in and to run it.
        </InlineAlert>
      ) : null}

      <UsageSection
        name={p.name}
        usage={usage.data?.agents?.[p.package] ?? undefined}
        loading={usage.isPending}
        failed={usage.isError ? errorText(usage.error) : ""}
      />

      {p.enabled && (methods.length || logins.isPending || logins.isError) ? (
        <PanelCard title="Sign-in">
          <p className="text-fg-secondary">
            Use your subscription instead of an API key. The credentials stay in{" "}
            {agentHome(p.skills_dir)}.
          </p>
          <SignInCell
            pkg={p}
            status={logins.data?.agents?.[p.package] ?? undefined}
            loading={logins.isPending}
            failed={logins.isError}
            onSignIn={(m) =>
              signIn.start({ agent: p.package, agentName: p.name, method: m.id, label: m.label })
            }
            onSignOut={signOut}
          />
        </PanelCard>
      ) : null}

      <KeysSection p={p} />
      <SettingsSection p={p} all={all} />

      {p.origin === "added" ? (
        <PanelCard title="Remove">
          <p className="text-fg-secondary">
            {p.name} was added from the registry. {removeBody(p)}
          </p>
          <div className="flex flex-wrap gap-2">
            {p.previous ? (
              <Button variant="outline" aria-label={`Roll back ${p.name}`} onClick={rollBack}>
                Roll back to {p.previous}
              </Button>
            ) : null}
            <Button variant="outline" aria-label={`Remove ${p.name}`} onClick={remove}>
              Remove
            </Button>
          </div>
        </PanelCard>
      ) : null}
      {signIn.dialog}
      {confirmDialog}
    </Page>
  );
}

function UsageSection({
  name,
  usage,
  loading,
  failed,
}: {
  name: string;
  usage: AgentUsage | undefined;
  loading: boolean;
  failed: string;
}) {
  let body: ReactNode;
  if (loading) body = <LoadingRows n={3} />;
  else if (failed) body = <InlineAlert tone="warn">Usage is unavailable: {failed}</InlineAlert>;
  else if (!usage?.tracked)
    body = (
      <EmptyState
        text={`Berth can't read ${name}'s usage yet: it doesn't know where ${name} records it.`}
      />
    );
  else {
    const models = usage.models ?? [];
    body = (
      <>
        {usage.error ? (
          <InlineAlert tone="warn">
            Some session files could not be read; the totals leave them out. {usage.error}
          </InlineAlert>
        ) : null}
        <div className="grid grid-cols-[repeat(auto-fit,minmax(140px,1fr))] gap-3">
          <Stat
            label="Today"
            value={tokens(usage.today.total)}
            detail={`tokens · ${usage.today.messages} responses`}
          />
          <Stat
            label="Last 7 days"
            value={tokens(usage.week.total)}
            detail={`tokens · ${usage.week.messages} responses`}
          />
          <Stat label="Sessions" value={String(usage.week.sessions)} detail="in the last 7 days" />
          <Stat
            label="Last active"
            value={usage.last_active ? lastActive(usage.last_active) : "—"}
            detail={
              usage.last_active
                ? new Date(usage.last_active).toLocaleString()
                : "No use in the last 7 days"
            }
          />
        </div>
        <UsageBars days={usage.days ?? []} total={usage.week.total} />
        {models.length ? (
          <Table aria-label="Usage by model">
            <TableHeader>
              <TableRow>
                <TableHead>Model</TableHead>
                <TableHead className="text-right">Responses</TableHead>
                <TableHead className="text-right">Input</TableHead>
                <TableHead className="text-right">Output</TableHead>
                <TableHead className="text-right">Cache read</TableHead>
                <TableHead className="text-right">Cache write</TableHead>
                <TableHead className="text-right">Total</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {models.map((m) => (
                <TableRow key={m.model}>
                  <TableCell data-label="">
                    <Mono>{m.model}</Mono>
                  </TableCell>
                  {(
                    [
                      ["Responses", m.messages.toLocaleString()],
                      ["Input", tokens(m.input)],
                      ["Output", tokens(m.output)],
                      ["Cache read", tokens(m.cache_read)],
                      ["Cache write", tokens(m.cache_write)],
                      ["Total", tokens(m.total)],
                    ] as const
                  ).map(([label, v]) => (
                    <TableCell key={label} data-label={label} className="text-right tabular-nums">
                      {v}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : null}
        <p className="text-xs text-fg-muted">
          Counted from {name}'s own session files in this container. Berth reads only token counts,
          models and times, never prompts or output. Cache reads are input served from the prompt
          cache.
        </p>
      </>
    );
  }
  return (
    <PanelCard title="Usage" badge={<span className="text-xs text-fg-muted">Last 7 days</span>}>
      {body}
    </PanelCard>
  );
}

function Stat({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 rounded-md bg-surface-sunken px-3 py-2.5">
      <span className="text-xs text-fg-muted">{label}</span>
      <span className="text-xl font-semibold tabular-nums">{value}</span>
      <span className="truncate text-xs text-fg-muted">{detail}</span>
    </div>
  );
}

function KeysSection({ p }: { p: AgentPackage }) {
  const keys = p.keys ?? [];
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
    <PanelCard title="API keys">
      {keys.length ? (
        <>
          <p className="text-fg-secondary">
            Berth passes these to {p.name} when it starts. Once saved, a key is never shown again.
          </p>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(280px,100%),1fr))] gap-3">
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
        </>
      ) : (
        <p className="text-fg-muted">{p.name} needs no API key.</p>
      )}
    </PanelCard>
  );
}

function SettingsSection({ p, all }: { p: AgentPackage; all: AgentPackage[] }) {
  const [pending, setPending] = useState<boolean | null>(null);
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
  const rows: [string, ReactNode][] = [];
  if (p.bins?.length) rows.push(["Command", <Mono key="bins">{p.bins.join(", ")}</Mono>]);
  if (p.instructions) rows.push(["Instructions", <Mono key="instructions">{p.instructions}</Mono>]);
  if (p.skills_dir) rows.push(["Skills", <Mono key="skills">{p.skills_dir}</Mono>]);
  if (p.skills_project)
    rows.push(["Workspace skills", <Mono key="skills-project">{p.skills_project}</Mono>]);
  if (p.mcp_project) rows.push(["Workspace MCP", <Mono key="mcp">{p.mcp_project}</Mono>]);
  return (
    <PanelCard title="Settings">
      <div className="flex items-center gap-3">
        <div className="flex flex-1 flex-col gap-0.5">
          <strong>Enabled</strong>
          <span className="text-fg-muted">
            Off removes its command; its settings and sign-in stay.
          </span>
        </div>
        <OnOffSwitch
          checked={pending ?? p.enabled}
          disabled={pending !== null}
          label={`${p.enabled ? "Turn off" : "Turn on"} ${p.name}`}
          onCheckedChange={(on) => void toggle(on)}
        />
      </div>
      {rows.length ? <Facts rows={rows} /> : null}
    </PanelCard>
  );
}
