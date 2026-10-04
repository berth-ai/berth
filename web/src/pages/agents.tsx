import { ChevronRight } from "lucide-react";
import { useId, useState } from "react";
import { applyConfig } from "@/api/config";
import { refreshLogins, refreshStatus, useConfig, useLogins, useStatus } from "@/api/queries";
import { verb } from "@/api/request";
import { runJob } from "@/api/run-job";
import type {
  AgentPackage,
  AgentVersion,
  Job,
  LoginMethodStatus,
  Status,
  UpdateCheck,
} from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
import { checkResult, versionLabel } from "./agents-logic";
import { PackageSection } from "./package-table";
import { useSignIn } from "./sign-in";

// Agents: update source and actions, version history, per-agent toggles and keys.
export function AgentsPage() {
  useTitle("Agents");
  const status = useStatus();
  const pkgs = status.data ? (status.data.packages ?? []) : undefined;
  const logins = useLogins();
  const signIn = useSignIn(logins.data, pkgs);
  const [confirmDialog, ask] = useConfirm();
  const signOut = (p: AgentPackage, m: LoginMethodStatus) =>
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
  return (
    <Page>
      <PageHeader
        title="Agents"
        description="Pinned agent CLIs. Updates install a new locked set; the previous one is kept for rollback."
      />
      <SourceCard />
      <History status={status.data} />
      <PackageSection
        title="Agents"
        kind="agent"
        nameHeader="Agent"
        noun={["agent", "agents"]}
        all={pkgs}
        empty="No agents installed yet."
        signIn={{
          data: logins.data,
          loading: logins.isPending,
          failed: logins.isError,
          onSignIn: (p, m) =>
            signIn.start({
              agent: p.package,
              agentName: p.name,
              icon: p.icon,
              method: m.id,
              label: m.label,
            }),
          onSignOut: signOut,
        }}
      />
      {signIn.dialog}
      {confirmDialog}
    </Page>
  );
}

function finishJob(job: Job | null, okText: string) {
  if (!job) return;
  notify(
    job.state === "succeeded" ? okText : "The job failed; open Activity for its log.",
    job.state === "succeeded" ? "ok" : "bad",
  );
  void refreshStatus();
}

/** startJob is a confirm action that starts a job and reports its outcome. */
const startJob = (name: string, okText: string) => async () => {
  void runJob(name, {}).then((job) => finishJob(job, okText));
  return true;
};

function SourceCard() {
  const config = useConfig();
  if (config.isPending) {
    return (
      <PanelCard>
        <LoadingRows n={2} />
      </PanelCard>
    );
  }
  return <SourceForm initialRef={config.data?.config.agents.ref ?? "main"} />;
}

function SourceForm({ initialRef }: { initialRef: string }) {
  const [ref, setRef] = useState(initialRef);
  const [value, setValue] = useState(initialRef);
  const [refError, setRefError] = useState("");
  const [saving, setSaving] = useState(false);
  const [checking, setChecking] = useState(false);
  const [result, setResult] = useState<{ tone: Tone; text: string } | null>(null);
  const [dialog, confirm] = useConfirm();
  const id = useId();

  const save = async () => {
    setSaving(true);
    try {
      const r = await applyConfig({ agents: { ref: value.trim() } });
      setRefError("");
      setRef(r.config.agents.ref);
      notify(`Updates now come from ${r.config.agents.ref}.`);
    } catch (err) {
      setRefError(fieldErrors(err)["agents.ref"] || errorText(err));
    }
    setSaving(false);
  };

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
      footer={
        <>
          <BusyButton
            variant="outline"
            busy={checking}
            busyLabel="Checking…"
            onClick={() => void check()}
          >
            Check for updates
          </BusyButton>
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
          <Button
            variant="outline"
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
      <div className="flex flex-wrap items-end gap-2">
        <FormField
          id={id}
          label="Update source"
          hint="A branch, tag or commit of the Berth repository."
          error={refError}
          className="flex-1"
        >
          <Input
            {...fieldProps(id, refError)}
            className="font-mono text-xs"
            spellCheck={false}
            autoComplete="off"
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
        </FormField>
        <BusyButton
          variant="outline"
          size="sm"
          busy={saving}
          busyLabel="Saving…"
          onClick={() => void save()}
          className="mb-5.5"
        >
          Save
        </BusyButton>
      </div>
      <div aria-live="polite">
        {result ? <InlineAlert tone={result.tone}>{result.text}</InlineAlert> : null}
      </div>
      {dialog}
    </PanelCard>
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
