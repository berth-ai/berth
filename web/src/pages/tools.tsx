import { Plus } from "lucide-react";
import { useState } from "react";
import { keys, queryClient, useTools } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { Job, Preset, Tool } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { CardGrid, Page, PageHeader, SectionTitle } from "@/components/page";
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
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { ToolDialog } from "./tool-dialog";

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

// Tools: the base track (read-only), the user track, and presets.
export function ToolsPage() {
  useTitle("Tools");
  const tools = useTools();
  const [dialog, setDialog] = useState<{ tool?: string; current?: string } | null>(null);
  const [confirmDialog, confirm] = useConfirm();
  const data = tools.data;
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
    <Page>
      <PageHeader
        title="Tools"
        description="The pinned toolchain, your own tools, and curated presets."
        actions={
          <Button onClick={() => setDialog({})}>
            <Plus aria-hidden />
            Add tool
          </Button>
        }
      />
      {tools.isError ? <InlineAlert tone="bad">{errorText(tools.error)}</InlineAlert> : null}
      {(data?.warnings ?? []).map((w) => (
        <InlineAlert key={w} tone="warn">
          {w}
        </InlineAlert>
      ))}
      <SectionTitle>Your tools</SectionTitle>
      {data ? (
        <UserTools
          tools={data.user}
          onUpgrade={(t) => setDialog({ tool: t.name, current: t.version })}
          onRemove={remove}
          onAdd={() => setDialog({})}
        />
      ) : (
        <LoadingRows n={4} />
      )}
      <SectionTitle>Presets</SectionTitle>
      <CardGrid>
        {(data?.presets ?? []).map((p) => (
          <PresetCard key={p.id} preset={p} />
        ))}
      </CardGrid>
      <SectionTitle>Built in</SectionTitle>
      {data ? <BaseTools tools={data.base} /> : null}
      {dialog ? (
        <ToolDialog
          tool={dialog.tool}
          current={dialog.current}
          onClose={() => setDialog(null)}
          onStarted={(t, v, job) => void job.then((j) => finish(j, `${t} ${v} installed.`))}
        />
      ) : null}
      {confirmDialog}
    </Page>
  );
}

function UserTools({
  tools,
  onUpgrade,
  onRemove,
  onAdd,
}: {
  tools: Tool[];
  onUpgrade: (t: Tool) => void;
  onRemove: (t: Tool) => void;
  onAdd: () => void;
}) {
  if (!tools.length) {
    return (
      <PanelCard>
        <EmptyState
          text="No tools added yet."
          action={
            <Button variant="outline" onClick={onAdd}>
              Add tool
            </Button>
          }
        />
      </PanelCard>
    );
  }
  return (
    <PanelCard>
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Tool</TableHead>
              <TableHead>Version</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {tools.map((t) => (
              <TableRow key={t.name}>
                <TableCell data-label="Tool">
                  <strong>{t.name}</strong>
                </TableCell>
                <TableCell data-label="Version">
                  <span className="font-mono text-xs">{t.version}</span>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-2">
                    <Button variant="outline" size="sm" onClick={() => onUpgrade(t)}>
                      Upgrade
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => onRemove(t)}>
                      Remove
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </PanelCard>
  );
}

function PresetCard({ preset }: { preset: Preset }) {
  const [busy, setBusy] = useState(false);
  const state = preset.state || "not_installed";
  const install = async () => {
    setBusy(true);
    const job = await runJob("preset.install", { preset: preset.id });
    setBusy(false);
    finish(job, `${preset.name} installed.`);
  };
  return (
    <PanelCard
      title={preset.name}
      badge={<StatusBadge state={state} />}
      footer={
        state === "installed" ? null : (
          <BusyButton size="sm" busy={busy} busyLabel="Installing…" onClick={() => void install()}>
            {state === "outdated" ? "Update" : "Install"}
          </BusyButton>
        )
      }
    >
      <p className="text-fg-muted">{preset.description}</p>
      <div>
        {Object.entries(preset.tools).map(([t, v]) => (
          <div key={t} className="font-mono text-xs">
            {t} {v}
          </div>
        ))}
      </div>
    </PanelCard>
  );
}

function BaseTools({ tools }: { tools: Tool[] }) {
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const rows = tools.filter((t) => t.name.toLowerCase().includes(q));
  return (
    <PanelCard
      title="Built in"
      badge={
        <span className="text-xs text-fg-muted">{tools.length} tools, updated with the image</span>
      }
      actions={
        <Input
          type="search"
          placeholder="Filter"
          aria-label="Filter built-in tools"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="w-48"
        />
      }
    >
      {rows.length ? (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tool</TableHead>
                <TableHead>Version</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((t) => (
                <TableRow key={t.name}>
                  <TableCell data-label="Tool">{t.name}</TableCell>
                  <TableCell data-label="Version">
                    <span className="font-mono text-xs">{t.version}</span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        <EmptyState text="No tool matches." />
      )}
    </PanelCard>
  );
}
