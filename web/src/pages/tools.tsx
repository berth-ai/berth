import { Plus } from "lucide-react";
import { useState } from "react";
import { keys, queryClient, useTools, useToolVersions } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { Job, Preset, Tool } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { errorText } from "@/lib/errors";
import { count } from "@/lib/format";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { ToolDialog } from "./tool-dialog";
import { TOOLS_TABS, parseToolsTab, presetPinned, upgradeTo, type ToolsTab } from "./tools-logic";

const TAB_LABELS: Record<ToolsTab, string> = {
  builtin: "Built-in",
  yours: "Your tools",
  presets: "Presets",
};

function reload() {
  void queryClient.invalidateQueries({ queryKey: keys.tools });
}

function finish(job: Job | null, okText: string) {
  if (!job) return;
  notify(
    job.state === "succeeded" ? okText : "The job failed; open Activity for its log.",
    job.state === "succeeded" ? "ok" : "bad",
  );
  reload();
}

// Tools: the base track (read-only), the user track, and presets, one tab each.
export function ToolsPage({ tab, onTab }: { tab: ToolsTab; onTab: (t: ToolsTab) => void }) {
  useTitle("Tools");
  const tools = useTools();
  const [adding, setAdding] = useState(false);
  const data = tools.data;
  const counts: Record<ToolsTab, number | undefined> = {
    builtin: data?.base.length,
    yours: data?.user.length,
    presets: data?.presets.length,
  };
  return (
    <Page>
      <PageHeader
        title="Tools"
        description="Built-in tools ship with the image. Tools you add live in your home volume and survive image updates."
      />
      {tools.isError ? <InlineAlert tone="bad">{errorText(tools.error)}</InlineAlert> : null}
      {(data?.warnings ?? []).map((w) => (
        <InlineAlert key={w} tone="warn">
          {w}
        </InlineAlert>
      ))}
      <ToggleGroup
        aria-label="Tool sections"
        value={[tab]}
        onValueChange={(value) => {
          if (value[0] !== undefined) onTab(parseToolsTab(value[0]));
        }}
        className="gap-0.5 rounded-md bg-surface-sunken p-0.5 max-sm:w-full"
      >
        {TOOLS_TABS.map((t) => (
          <ToggleGroupItem
            key={t}
            value={t}
            className="h-7 gap-1.5 rounded-sm px-3 font-medium text-fg-muted data-pressed:bg-surface data-pressed:text-fg data-pressed:shadow-xs max-sm:flex-1 max-sm:px-2"
          >
            {TAB_LABELS[t]}
            {counts[t] === undefined ? null : (
              <span className="text-xs text-fg-muted tabular-nums">{counts[t]}</span>
            )}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      {!data ? (
        tools.isError ? null : (
          <PanelCard>
            <LoadingRows n={4} />
          </PanelCard>
        )
      ) : tab === "builtin" ? (
        <BuiltinTab tools={data.base} />
      ) : tab === "yours" ? (
        <YoursTab tools={data.user} onAdd={() => setAdding(true)} />
      ) : (
        <PresetsTab presets={data.presets} />
      )}
      {adding ? (
        <ToolDialog
          onClose={() => setAdding(false)}
          onStarted={(t, v, job) => void job.then((j) => finish(j, `${t} ${v} installed.`))}
        />
      ) : null}
    </Page>
  );
}

function BuiltinTab({ tools }: { tools: Tool[] }) {
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const rows = tools.filter((t) => t.name.toLowerCase().includes(q));
  return (
    <PanelCard
      title="Built-in"
      badge={
        <span className="text-xs text-fg-muted">
          {count(tools.length, "tool", "tools")} · pinned in the image · read-only
        </span>
      }
      actions={
        <Input
          type="search"
          placeholder="Search tools"
          aria-label="Search built-in tools"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="w-48 max-sm:w-full"
        />
      }
    >
      {rows.length ? (
        <Table aria-label="Built-in tools">
          <TableHeader>
            <TableRow>
              <TableHead>Tool</TableHead>
              <TableHead>Version</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((t) => (
              <TableRow key={t.name}>
                <TableCell data-label="">
                  <strong className="text-fg">{t.name}</strong>
                </TableCell>
                <TableCell data-label="Version">
                  <span className="font-mono text-xs">{t.version}</span>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          text={`No built-in tools match “${query.trim()}”. You can add it under Your tools.`}
        />
      )}
    </PanelCard>
  );
}

function YoursTab({ tools, onAdd }: { tools: Tool[]; onAdd: () => void }) {
  const [confirmDialog, confirm] = useConfirm();
  const remove = (t: Tool) =>
    confirm({
      title: `Remove ${t.name}?`,
      body: `Berth runs mise unuse -g ${t.name} as the agent user. Projects that pin it keep their own version.`,
      confirm: "Remove tool",
      danger: true,
      action: async () => {
        void runJob("tools.remove", { tool: t.name }).then((job) =>
          finish(job, `${t.name} removed.`),
        );
        return true;
      },
    });
  return (
    <PanelCard
      title="Your tools"
      actions={
        <Button size="sm" onClick={onAdd}>
          <Plus aria-hidden />
          Add tool
        </Button>
      }
    >
      {tools.length ? (
        <Table aria-label="Your tools">
          <TableHeader>
            <TableRow>
              <TableHead>Tool</TableHead>
              <TableHead>Version</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tools.map((t) => (
              <UserToolRow key={t.name} t={t} onRemove={() => remove(t)} />
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          text="No tools added yet. Add one from the registry and pick a version; it installs in your home volume."
          action={
            <Button variant="outline" onClick={onAdd}>
              Add your first tool
            </Button>
          }
        />
      )}
      {confirmDialog}
    </PanelCard>
  );
}

function UserToolRow({ t, onRemove }: { t: Tool; onRemove: () => void }) {
  const versions = useToolVersions(t.name);
  const [busy, setBusy] = useState(false);
  const target = upgradeTo(t.version, versions.data?.versions);
  const state = versions.isPending ? "checking" : target ? "outdated" : "installed";
  const upgrade = async () => {
    if (!target) return;
    setBusy(true);
    const job = await runJob("tools.add", { tool: t.name, version: target });
    setBusy(false);
    finish(job, `${t.name} ${target} installed.`);
  };
  return (
    <TableRow>
      <TableCell data-label="">
        <strong className="text-fg">{t.name}</strong>
      </TableCell>
      <TableCell data-label="Version">
        <span className="font-mono text-xs">{t.version}</span>
      </TableCell>
      <TableCell data-label="Status">
        <StatusBadge small state={state} detail={target} />
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap items-center justify-end gap-2 max-sm:justify-start">
          {target ? (
            <BusyButton
              variant="outline"
              size="sm"
              busy={busy}
              busyLabel="Upgrading…"
              onClick={() => void upgrade()}
            >
              Upgrade
            </BusyButton>
          ) : null}
          <Button variant="outline" size="sm" onClick={onRemove}>
            Remove
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}

function PresetsTab({ presets }: { presets: Preset[] }) {
  if (!presets.length) {
    return (
      <PanelCard>
        <EmptyState text="This image ships no presets." />
      </PanelCard>
    );
  }
  return (
    <PanelCard title="Presets">
      <Table aria-label="Presets">
        <TableHeader>
          <TableRow>
            <TableHead>Preset</TableHead>
            <TableHead>Pinned</TableHead>
            <TableHead>Includes</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>
              <span className="sr-only">Actions</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {presets.map((p) => (
            <PresetRow key={p.id} preset={p} />
          ))}
        </TableBody>
      </Table>
    </PanelCard>
  );
}

function PresetRow({ preset }: { preset: Preset }) {
  const [busy, setBusy] = useState(false);
  const state = preset.state || "not_installed";
  const install = async () => {
    setBusy(true);
    const job = await runJob("preset.install", { preset: preset.id });
    setBusy(false);
    finish(job, `${preset.name} installed.`);
  };
  return (
    <TableRow>
      <TableCell data-label="">
        <div className="flex min-w-0 flex-col gap-0.5">
          <strong className="text-fg">{preset.name}</strong>
          <span className="text-xs text-fg-muted">{preset.description}</span>
        </div>
      </TableCell>
      <TableCell data-label="Pinned">
        <span className="font-mono text-xs">{presetPinned(preset)}</span>
      </TableCell>
      <TableCell data-label="Includes">
        <span className="font-mono text-xs">
          {Object.keys(preset.tools)
            .sort()
            .map((t) => `${t} ${preset.tools[t]}`)
            .join(", ")}
        </span>
      </TableCell>
      <TableCell data-label="Status">
        <StatusBadge small state={state} />
      </TableCell>
      <TableCell className="text-right">
        {state === "installed" ? null : (
          <BusyButton
            size="sm"
            variant={state === "outdated" ? "outline" : "default"}
            busy={busy}
            busyLabel="Installing…"
            onClick={() => void install()}
          >
            {state === "outdated" ? "Update" : "Install"}
          </BusyButton>
        )}
      </TableCell>
    </TableRow>
  );
}
