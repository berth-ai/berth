import { Link } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";
import { useId, useState } from "react";
import { applyConfig } from "@/api/config";
import {
  refreshAgentRegistry,
  refreshLogins,
  refreshStatus,
  useAgentRegistry,
  useConfig,
  useLogins,
  useStatus,
  useUsage,
} from "@/api/queries";
import { verb } from "@/api/request";
import { runJob } from "@/api/run-job";
import type {
  AgentPackage,
  AgentUsage,
  AgentVersion,
  Job,
  LoginStatus,
  Status,
  UpdateCheck,
} from "@/api/types";
import { BrandIcon } from "@/components/brand-icon";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { Facts, Mono } from "@/components/facts";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { CardGrid, Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorText, fieldErrors } from "@/lib/errors";
import { describeSource, short, when } from "@/lib/format";
import { notify, type Tone } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { AddAgentDialog } from "./agent-dialog";
import {
  addedToast,
  agentSlug,
  allAdded,
  checkResult,
  signInSummary,
  usageFacts,
  versionLabel,
} from "./agents-logic";

// Agents: one card per agent with its status and usage, then the agent
// set's updates and history. Each agent's settings live on its own page.
export function AgentsPage() {
  useTitle("Agents");
  const status = useStatus();
  const agents = status.data
    ? (status.data.packages ?? []).filter((p) => p.kind === "agent")
    : undefined;
  const logins = useLogins();
  const usage = useUsage();
  const registry = useAgentRegistry();
  const [adding, setAdding] = useState(false);
  return (
    <Page>
      <PageHeader
        title="Agents"
        description="The agent CLIs Berth installs: who is signed in and how much each used. Open one to sign in, set keys or switch it off."
        actions={
          allAdded(registry.data?.agents) ? null : (
            <Button variant="outline" onClick={() => setAdding(true)}>
              Add agent
            </Button>
          )
        }
      />
      {usage.isError ? (
        <InlineAlert tone="warn">Usage is unavailable: {errorText(usage.error)}</InlineAlert>
      ) : null}
      {!agents ? (
        <PanelCard>
          <LoadingRows n={2} />
        </PanelCard>
      ) : agents.length ? (
        <CardGrid>
          {agents.map((p) => (
            <AgentCard
              key={p.package}
              p={p}
              slug={agentSlug(p.package, agents)}
              logins={logins.data}
              loginsLoading={logins.isPending}
              loginsFailed={logins.isError}
              usage={usage.data?.agents?.[p.package] ?? undefined}
              usageLoading={usage.isPending}
            />
          ))}
        </CardGrid>
      ) : (
        <PanelCard>
          <EmptyState text="No agents installed yet." />
        </PanelCard>
      )}
      <UpdatesCard status={status.data} />
      <History status={status.data} />
      {adding ? (
        <AddAgentDialog
          entries={registry.data?.agents}
          failed={registry.isError ? errorText(registry.error) : ""}
          onClose={() => setAdding(false)}
          onAdded={(name) => {
            notify(addedToast(name));
            void refreshStatus();
            void refreshLogins();
            void refreshAgentRegistry();
          }}
        />
      ) : null}
    </Page>
  );
}

/** AgentCard is an agent at a glance: sign-in, version and usage. */
function AgentCard({
  p,
  slug,
  logins,
  loginsLoading,
  loginsFailed,
  usage,
  usageLoading,
}: {
  p: AgentPackage;
  slug: string;
  logins: LoginStatus | undefined;
  loginsLoading: boolean;
  loginsFailed: boolean;
  usage: AgentUsage | undefined;
  usageLoading: boolean;
}) {
  const s = signInSummary(p.enabled, logins?.agents?.[p.package]?.methods, loginsLoading);
  const badge =
    s.state === "loading" ? (
      <Skeleton className="h-4 w-20" />
    ) : s.state === "none" || (loginsFailed && s.state !== "off") ? null : (
      <StatusBadge state={s.state} detail={s.account} />
    );
  return (
    <PanelCard
      title={p.name}
      icon={<BrandIcon slug={p.icon} name={p.name} id={p.package} />}
      badge={badge}
      footer={
        <Link
          to="/agents/$id"
          params={{ id: slug }}
          className={buttonVariants({ variant: "outline", size: "sm" })}
          aria-label={`Manage ${p.name}`}
        >
          Manage
        </Link>
      }
    >
      <span className="flex flex-wrap items-center gap-2 font-mono text-xs break-all text-fg-muted">
        {p.package} {p.version}
        {p.origin === "added" ? <StatusBadge small state="added" /> : null}
      </span>
      {p.error ? <span className="text-xs text-warn">{p.error}</span> : null}
      {usageLoading ? <LoadingRows n={3} /> : usage ? <Facts rows={usageFacts(usage)} /> : null}
    </PanelCard>
  );
}

function finishJob(job: Job | null, okText: string) {
  if (!job) return;
  notify(
    job.state === "succeeded" ? okText : "The job failed; open Activity for its log.",
    job.state === "succeeded" ? "ok" : "bad",
  );
  void refreshStatus();
  if (job.verb === "agents.rollback") void refreshAgentRegistry();
}

/** startJob is a confirm action that starts a job and reports its outcome. */
export const startJob =
  (name: string, okText: string, body: unknown = {}) =>
  async () => {
    void runJob(name, body).then((job) => finishJob(job, okText));
    return true;
  };

/** UpdatesCard is the agent set: where updates come from, what is
 * installed, and the actions that change it. */
function UpdatesCard({ status }: { status: Status | undefined }) {
  const config = useConfig();
  const [saved, setSaved] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [checking, setChecking] = useState(false);
  const [result, setResult] = useState<{ tone: Tone; text: string } | null>(null);
  const [dialog, confirm] = useConfirm();
  const ref = saved ?? config.data?.config.agents.ref ?? "main";
  const cur = status?.agents?.current;
  const prev = status?.agents?.previous;

  const check = async () => {
    setChecking(true);
    try {
      setResult(
        checkResult((await verb<{ check: UpdateCheck }>("update.check", { force: true })).check),
      );
    } catch (err) {
      setResult({ tone: "bad", text: errorText(err) });
    }
    setChecking(false);
    void refreshStatus();
  };

  return (
    <PanelCard
      title="Updates"
      badge={status?.check?.update_available ? <StatusBadge state="update" /> : null}
      footer={
        <>
          <Button
            onClick={() =>
              confirm({
                title: `Update the agents from ${ref}?`,
                body: "The new set installs beside the current one and switches atomically. Running agent sessions keep their version.",
                confirm: "Update",
                action: async () => {
                  void runJob("update", {}).then((job) => finishJob(job, "Agents updated."));
                  notify("Update started; follow it in Activity.", "info");
                  return true;
                },
              })
            }
          >
            Update
          </Button>
          <BusyButton
            variant="outline"
            busy={checking}
            busyLabel="Checking…"
            onClick={() => void check()}
          >
            Check for updates
          </BusyButton>
          <Button
            variant="outline"
            disabled={!prev}
            onClick={() =>
              confirm({
                title: "Roll back to the previous agent set?",
                body: "Berth switches back at once, offline. Restarts keep this choice until the next update.",
                confirm: "Roll back",
                danger: true,
                action: startJob("rollback", "Rolled back."),
              })
            }
          >
            Roll back
          </Button>
        </>
      }
    >
      {config.isPending || !status ? (
        <LoadingRows n={3} />
      ) : (
        <Facts
          rows={[
            [
              "Source",
              <span key="source" className="inline-flex flex-wrap items-center gap-2">
                <Mono>{ref}</Mono>
                <Button
                  variant="ghost"
                  size="xs"
                  aria-label="Change the update source"
                  onClick={() => setEditing(true)}
                >
                  Change
                </Button>
              </span>,
            ],
            [
              "Current",
              cur ? (
                <span key="current">
                  <Mono>{short(cur.hash)}</Mono> · installed {when(cur.installed_at)}
                </span>
              ) : (
                "No agent set is installed yet."
              ),
            ],
            ["Previous", prev ? <Mono key="previous">{short(prev.hash)}</Mono> : "None"],
          ]}
        />
      )}
      <div aria-live="polite">
        {result ? <InlineAlert tone={result.tone}>{result.text}</InlineAlert> : null}
      </div>
      {editing ? (
        <SourceDialog
          initialRef={ref}
          onClose={() => setEditing(false)}
          onSaved={(r) => {
            setSaved(r);
            setResult(null);
          }}
        />
      ) : null}
      {dialog}
    </PanelCard>
  );
}

function SourceDialog({
  initialRef,
  onClose,
  onSaved,
}: {
  initialRef: string;
  onClose: () => void;
  onSaved: (ref: string) => void;
}) {
  const [value, setValue] = useState(initialRef);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const id = useId();

  const save = async () => {
    setSaving(true);
    try {
      const r = await applyConfig({ agents: { ref: value.trim() } });
      notify(`Updates now come from ${r.config.agents.ref}.`);
      onSaved(r.config.agents.ref);
      onClose();
    } catch (err) {
      setError(fieldErrors(err)["agents.ref"] || errorText(err));
      setSaving(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Change the update source</DialogTitle>
        </DialogHeader>
        <form
          className="contents"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <FormField
            id={id}
            label="Update source"
            hint="A branch, tag or commit of the Berth repository."
            error={error}
          >
            <Input
              {...fieldProps(id, error)}
              className="font-mono text-xs"
              spellCheck={false}
              autoComplete="off"
              value={value}
              onChange={(e) => setValue(e.target.value)}
            />
          </FormField>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <BusyButton type="submit" busy={saving} busyLabel="Saving…">
              Save
            </BusyButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function History({ status }: { status: Status | undefined }) {
  const [dialog, confirm] = useConfirm();
  const versions = status?.agents?.versions ?? [];
  return (
    <PanelCard
      title="Version history"
      actions={
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            confirm({
              title: "Remove unused agent sets?",
              body: "Current, previous, any set in use, and the configured number of kept sets stay.",
              confirm: "Clean up",
              action: startJob("gc", "Old versions removed."),
            })
          }
        >
          Clean up old versions
        </Button>
      }
    >
      {!status ? (
        <LoadingRows n={3} />
      ) : versions.length ? (
        <Table aria-label="Version history">
          <TableHeader>
            <TableRow>
              <TableHead>Status</TableHead>
              <TableHead>Lock</TableHead>
              <TableHead className="max-sm:hidden">Origin</TableHead>
              <TableHead>Installed</TableHead>
              <TableHead>
                <span className="sr-only">Packages</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {versions.map((v) => (
              <VersionRows
                key={v.hash}
                v={v}
                label={versionLabel(
                  v.hash,
                  status.agents?.current?.hash,
                  status.agents?.previous?.hash,
                )}
              />
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState text="No agent set is installed yet." />
      )}
      {dialog}
    </PanelCard>
  );
}

function VersionRows({ v, label }: { v: AgentVersion; label: string }) {
  const [open, setOpen] = useState(false);
  const detailId = useId();
  const pkgs = Object.keys(v.packages ?? {}).sort();
  return (
    <>
      <TableRow>
        <TableCell data-label="">
          <strong className="text-fg">{label}</strong>
        </TableCell>
        <TableCell data-label="Lock">
          <span className="font-mono text-xs">{short(v.hash)}</span>
        </TableCell>
        <TableCell data-label="Origin" className="max-sm:hidden!">
          {describeSource(v.source)}
        </TableCell>
        <TableCell data-label="Installed">{when(v.installed_at)}</TableCell>
        <TableCell className="text-right">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-expanded={open}
            aria-controls={open ? detailId : undefined}
            aria-label={`Package versions for ${short(v.hash)}`}
            onClick={() => setOpen((o) => !o)}
          >
            <ChevronRight
              aria-hidden
              className={
                open ? "size-3.5 rotate-90 transition-transform" : "size-3.5 transition-transform"
              }
            />
          </Button>
        </TableCell>
      </TableRow>
      {open ? (
        <TableRow id={detailId} className="hover:bg-transparent">
          <TableCell colSpan={5}>
            {pkgs.length ? (
              <div className="grid grid-cols-[repeat(auto-fill,minmax(min(240px,100%),1fr))] gap-x-4 gap-y-0.5">
                {pkgs.map((n) => (
                  <div key={n} className="flex justify-between gap-3 font-mono text-xs">
                    <span className="break-all">{n}</span>
                    <span className="text-fg-muted">{v.packages?.[n]}</span>
                  </div>
                ))}
              </div>
            ) : (
              <span className="text-xs text-fg-muted">No package versions recorded.</span>
            )}
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}
