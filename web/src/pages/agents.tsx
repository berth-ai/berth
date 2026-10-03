import { ChevronRight } from "lucide-react";
import { useId, useState } from "react";
import { applyConfig } from "@/api/config";
import { refreshStatus, useConfig, useStatus } from "@/api/queries";
import { verb } from "@/api/request";
import { runJob } from "@/api/run-job";
import type { AgentPackage, Job, Status, UpdateCheck } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { OnOffSwitch } from "@/components/on-off-switch";
import { CardGrid, Page, PageHeader, SectionTitle } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { SecretField } from "@/components/secret-field";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { errorText, fieldErrors } from "@/lib/errors";
import { describeSource, short, when } from "@/lib/format";
import { notify, type Tone } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { checkResult, disabledAfter, versionLabel } from "./agents-logic";

// Agents: update source and actions, per-package toggles and keys, history.
export function AgentsPage() {
  useTitle("Agents");
  const status = useStatus();
  const pkgs = status.data ? (status.data.packages ?? []) : undefined;
  return (
    <Page>
      <PageHeader
        title="Agents"
        description="Pinned agent CLIs and MCP servers. Switch each on or off and give them API keys."
      />
      <SourceCard />
      <SectionTitle>Agents</SectionTitle>
      <PackageGrid all={pkgs} kind="agent" empty="No agents installed yet." />
      <SectionTitle>MCP servers</SectionTitle>
      <PackageGrid all={pkgs} kind="mcp" empty="No MCP servers installed yet." />
      <SectionTitle>History</SectionTitle>
      <History status={status.data} />
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

  const start = (name: string, okText: string) => async () => {
    void runJob(name, {}).then((job) => finishJob(job, okText));
    return true;
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
                action: start("rollback", "Rolled back."),
              })
            }
          >
            Roll back
          </Button>
          <Button
            variant="outline"
            onClick={() =>
              confirm({
                title: "Remove unused agent sets?",
                body: "Current, previous, any set in use, and the configured number of kept sets stay.",
                confirm: "Clean up",
                action: start("gc", "Old versions removed."),
              })
            }
          >
            Clean up old versions
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

function PackageGrid({
  all,
  kind,
  empty,
}: {
  all: AgentPackage[] | undefined;
  kind: string;
  empty: string;
}) {
  if (!all) return <LoadingRows n={2} />;
  const list = all.filter((p) => p.kind === kind);
  if (!list.length) return <EmptyState text={empty} />;
  return (
    <CardGrid>
      {list.map((p) => (
        <PackageCard key={p.package} p={p} all={all} />
      ))}
    </CardGrid>
  );
}

function PackageCard({ p, all }: { p: AgentPackage; all: AgentPackage[] }) {
  const [pending, setPending] = useState<boolean | null>(null);
  const toggle = async (on: boolean) => {
    setPending(on);
    try {
      await applyConfig({ agents: { disabled: disabledAfter(all, p.package, on) } });
      notify(`${p.name}${on ? " is on." : " is off."} MCP registrations update in the background.`);
      await refreshStatus();
    } catch (err) {
      notify(errorText(err), "bad");
    }
    setPending(null);
  };
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
    <PanelCard
      title={p.name}
      badge={<StatusBadge state={p.enabled ? "on" : "off"} />}
      actions={
        <OnOffSwitch
          checked={pending ?? p.enabled}
          disabled={pending !== null}
          label={`${p.enabled ? "Turn off" : "Turn on"} ${p.name}`}
          onCheckedChange={(on) => void toggle(on)}
        />
      }
    >
      <p className="text-fg-muted">
        <span className="font-mono text-xs">
          {p.package} {p.version}
        </span>
      </p>
      {keys.length ? (
        keys.map((k) => (
          <SecretField
            key={k.name}
            name={k.name}
            isSet={k.set}
            onSave={(value) => saveKey(k.name, value, `${k.name} saved.`)()}
            onRemove={saveKey(k.name, null, `${k.name} removed.`)}
          />
        ))
      ) : (
        <p className="text-xs text-fg-muted">Needs no API key.</p>
      )}
    </PanelCard>
  );
}

function History({ status }: { status: Status | undefined }) {
  if (!status) return <LoadingRows n={3} />;
  const versions = status.agents?.versions ?? [];
  if (!versions.length) {
    return (
      <PanelCard>
        <EmptyState text="No agent set is installed yet." />
      </PanelCard>
    );
  }
  return (
    <Card className="gap-0 py-0">
      {versions.map((v) => (
        <Collapsible key={v.hash} className="border-b last:border-b-0">
          <CollapsibleTrigger className="group grid w-full cursor-pointer grid-cols-[90px_110px_minmax(0,1fr)_auto] items-center gap-3 px-4 py-2 text-left hover:bg-surface-sunken max-sm:grid-cols-[minmax(0,1fr)_auto]">
            <strong className="flex items-center gap-1">
              <ChevronRight
                aria-hidden
                className="size-3.5 transition-transform group-data-panel-open:rotate-90"
              />
              {versionLabel(v.hash, status.agents?.current?.hash, status.agents?.previous?.hash)}
            </strong>
            <span className="font-mono text-xs">{short(v.hash)}</span>
            <span className="max-sm:hidden">{describeSource(v.source)}</span>
            <span className="text-fg-muted">{when(v.installed_at)}</span>
          </CollapsibleTrigger>
          <CollapsibleContent className="px-4 pt-1 pb-3">
            {Object.keys(v.packages ?? {})
              .sort()
              .map((n) => (
                <div key={n} className="font-mono text-xs">
                  {n} {v.packages?.[n]}
                </div>
              ))}
          </CollapsibleContent>
        </Collapsible>
      ))}
    </Card>
  );
}
