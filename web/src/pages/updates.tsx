import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Ellipsis } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { refreshStatus, useStatus } from "@/api/queries";
import { verb } from "@/api/request";
import { runJob } from "@/api/run-job";
import type { AgentVersion, Job, Pin, Status, UpdateCheck } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { Facts, Mono } from "@/components/facts";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorText } from "@/lib/errors";
import { describeSource, short, when } from "@/lib/format";
import { notify, type Tone } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { versionLabel } from "./agents-logic";
import {
  checkSummary,
  newestFirst,
  picked,
  pinRows,
  setBefore,
  setChanges,
  plural,
  updatable,
  updateAllBody,
} from "./updates-logic";

function finishJob(job: Job | null, okText: string) {
  if (!job) return;
  notify(
    job.state === "succeeded" ? okText : "The job failed; open Activity for its log.",
    job.state === "succeeded" ? "ok" : "bad",
  );
  void refreshStatus();
}

/** start runs an update or rollback job and reports how it ended. */
function start(name: string, body: unknown, okText: string) {
  void runJob(name, body).then((job) => finishJob(job, okText));
  notify("Started; follow it in Activity.", "info");
}

// Updates: every entity the image pins (agent CLIs, MCP servers, base
// tools) with the image's version, what runs, and the newest upstream; the
// installed sets and their history.
export function UpdatesPage() {
  useTitle("Updates");
  const status = useStatus();
  const st = status.data;
  const [checking, setChecking] = useState(false);
  const [result, setResult] = useState<{ tone: Tone; text: string } | null>(null);
  const [pick, setPick] = useState<Pin | null>(null);
  const [dialog, confirm] = useConfirm();
  const agents = pinRows(st?.agents, st?.check, "agents");
  const tools = pinRows(st?.agents, st?.check, "tools");
  const all = updatable([...agents, ...tools]);
  const anyPicked = [...agents, ...tools].some(picked);

  const check = async (force: boolean) => {
    setChecking(true);
    try {
      const c = (await verb<{ check: UpdateCheck }>("update.check", { force })).check;
      if (force || c.update_available === null) setResult(checkSummary(c));
    } catch (err) {
      setResult({ tone: "bad", text: errorText(err) });
    }
    setChecking(false);
    void refreshStatus();
  };

  // The broker keeps a check for an hour; opening the page asks for it.
  const asked = useRef(false);
  useEffect(() => {
    if (asked.current) return;
    asked.current = true;
    void check(false);
  }, []);

  const updateOne = (p: Pin, version?: string) => {
    const to = version ?? p.latest ?? "";
    const run = () => start("update", { names: [`${p.name}@${to}`] }, `${p.title} is at ${to}.`);
    if (version === undefined && p.update !== "major") return run();
    confirm({
      title: `Move ${p.title} to ${to}?`,
      body:
        p.update === "major" && version === undefined
          ? `This is a major version: ${p.title} may behave differently. Roll back if it does.`
          : `It installs beside the current version and switches atomically. A newer image keeps it until it ships ${to} or later.`,
      confirm: "Install",
      action: async () => {
        run();
        return true;
      },
    });
  };

  const toImage = (p: Pin) =>
    confirm({
      title: `Use the image's ${p.title}?`,
      body: `${p.title} returns to ${p.image} and follows the image again.`,
      confirm: "Use the image's",
      action: async () => {
        start("update", { image: true, names: [p.name] }, `${p.title} follows the image.`);
        return true;
      },
    });

  return (
    <Page>
      <PageHeader
        title="Updates"
        description="Agent CLIs, MCP servers and base tools start at the versions the image pins. Pick newer ones here; a newer image takes over once it ships them."
        actions={
          <>
            <BusyButton
              variant="outline"
              busy={checking}
              busyLabel="Checking…"
              onClick={() => void check(true)}
            >
              Check for updates
            </BusyButton>
            <Button
              disabled={!all.length}
              onClick={() =>
                confirm({
                  title: `Install ${plural(all.length, "update", "updates")}?`,
                  body: updateAllBody(all),
                  confirm: "Update all",
                  action: async () => {
                    // The versions the dialog listed, not whatever is newest by then.
                    start(
                      "update",
                      { names: all.map((p) => `${p.name}@${p.latest}`) },
                      "Updates installed.",
                    );
                    return true;
                  },
                })
              }
            >
              Update all
            </Button>
          </>
        }
      />
      <div aria-live="polite">
        {result ? <InlineAlert tone={result.tone}>{result.text}</InlineAlert> : null}
      </div>
      <PinTable
        title="Agents and MCP servers"
        label="Agent packages"
        rows={agents}
        loading={!st}
        checking={checking && !st?.check}
        onUpdate={updateOne}
        onPick={setPick}
        onImage={toImage}
      />
      <PinTable
        title="Base tools"
        label="Base tools"
        rows={tools}
        loading={!st}
        checking={checking && !st?.check}
        onUpdate={updateOne}
        onPick={setPick}
        onImage={toImage}
      />
      <SetsCard status={st} anyPicked={anyPicked} confirm={confirm} />
      <History status={st} />
      {pick ? (
        <PickDialog
          pin={pick}
          onClose={() => setPick(null)}
          onPicked={(v) => {
            setPick(null);
            updateOne(pick, v);
          }}
        />
      ) : null}
      {dialog}
    </Page>
  );
}

function PinTable({
  title,
  label,
  rows,
  loading,
  checking,
  onUpdate,
  onPick,
  onImage,
}: {
  title: string;
  label: string;
  rows: Pin[];
  loading: boolean;
  checking: boolean;
  onUpdate: (p: Pin) => void;
  onPick: (p: Pin) => void;
  onImage: (p: Pin) => void;
}) {
  const n = updatable(rows).length;
  return (
    <PanelCard
      title={title}
      badge={
        n ? (
          <StatusBadge state="update" />
        ) : (
          <span className="text-xs text-fg-muted">{plural(rows.length, "entry", "entries")}</span>
        )
      }
    >
      {loading ? (
        <LoadingRows n={3} />
      ) : rows.length ? (
        <Table aria-label={label}>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Image</TableHead>
              <TableHead>Running</TableHead>
              <TableHead>Newest</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((p) => (
              <TableRow key={p.name}>
                <TableCell data-label="">
                  <strong className="block text-fg">{p.title}</strong>
                  {p.title !== p.name ? (
                    <span className="font-mono text-xs break-all text-fg-muted">{p.name}</span>
                  ) : null}
                </TableCell>
                <TableCell data-label="Image">
                  <Mono>{p.image}</Mono>
                </TableCell>
                <TableCell data-label="Running">
                  <Mono>{p.current}</Mono>
                  {picked(p) ? <span className="text-xs text-fg-muted"> · your pick</span> : null}
                </TableCell>
                <TableCell data-label="Newest">
                  <Newest p={p} checking={checking} />
                </TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  <span className="inline-flex items-center gap-1">
                    {p.update ? (
                      <Button
                        variant="outline"
                        size="sm"
                        aria-label={`Update ${p.title} to ${p.latest}`}
                        onClick={() => onUpdate(p)}
                      >
                        Update
                      </Button>
                    ) : null}
                    <DropdownMenu>
                      <DropdownMenuTrigger
                        render={
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label={`More for ${p.title}`}
                          />
                        }
                      >
                        <Ellipsis aria-hidden className="size-3.5" strokeWidth={1.6} />
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => onPick(p)}>
                          Pick a version…
                        </DropdownMenuItem>
                        <DropdownMenuItem disabled={!picked(p)} onClick={() => onImage(p)}>
                          Use the image's version
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </span>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState text="This image pins nothing here." />
      )}
    </PanelCard>
  );
}

function Newest({ p, checking }: { p: Pin; checking: boolean }) {
  if (p.error)
    return (
      <span className="text-xs text-warn" title={p.error}>
        Could not look up
      </span>
    );
  if (!p.latest)
    return <span className="text-xs text-fg-muted">{checking ? "Checking…" : "Not checked"}</span>;
  if (!p.update)
    return (
      <span className="text-xs text-fg-muted">
        <Mono>{p.latest}</Mono> · up to date
      </span>
    );
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      <Mono>{p.latest}</Mono>
      {p.update === "major" ? (
        <span className="text-xs text-warn">Major update</span>
      ) : (
        <StatusBadge state="update" />
      )}
    </span>
  );
}

function SetsCard({
  status,
  anyPicked,
  confirm,
}: {
  status: Status | undefined;
  anyPicked: boolean;
  confirm: ReturnType<typeof useConfirm>[1];
}) {
  const cur = status?.agents?.current;
  const prev = status?.agents?.previous;
  const tools = status?.agents?.tools;
  const rollback = (tools: boolean) =>
    confirm({
      title: tools ? "Roll back the base tools?" : "Roll back the agents?",
      body: "Berth switches back to the previous set at once, offline. Restarts keep it.",
      confirm: "Roll back",
      danger: true,
      action: async () => {
        start("rollback", tools ? { tools: true } : {}, "Rolled back.");
        return true;
      },
    });
  return (
    <PanelCard
      title="Installed sets"
      footer={
        <>
          <Button variant="outline" disabled={!prev} onClick={() => rollback(false)}>
            Roll back agents
          </Button>
          <Button variant="outline" disabled={!tools?.previous} onClick={() => rollback(true)}>
            Roll back base tools
          </Button>
          <Button
            variant="outline"
            disabled={!anyPicked}
            onClick={() =>
              confirm({
                title: "Return everything to the image?",
                body: "Every agent, MCP server and base tool goes back to the version the image pins and follows the image again.",
                confirm: "Use the image's versions",
                action: async () => {
                  start("update", { image: true }, "Everything follows the image.");
                  return true;
                },
              })
            }
          >
            Use the image's versions
          </Button>
        </>
      }
    >
      {!status ? (
        <LoadingRows n={3} />
      ) : (
        <Facts
          rows={[
            [
              "Agents",
              cur ? (
                <span key="agents" className="flex flex-col gap-0.5">
                  <span>
                    {setChanges(cur, prev ?? undefined, pinTitles(status)) ||
                      "Same versions as before"}
                  </span>
                  <span className="text-xs text-fg-muted">
                    <Mono>{short(cur.hash)}</Mono> · {describeSource(cur.source)} · installed{" "}
                    {when(cur.installed_at)}
                  </span>
                </span>
              ) : (
                "No agent set is installed yet."
              ),
            ],
            ["Previous", prev ? <Mono key="prev">{short(prev.hash)}</Mono> : "None"],
            [
              "Base tools",
              tools?.current ? (
                <span key="tools">
                  <Mono>{toolList(tools.current)}</Mono> · installed{" "}
                  {when(tools.current.installed_at)}
                </span>
              ) : (
                "As in the image"
              ),
            ],
            [
              "Previous ",
              tools?.previous ? <Mono key="tprev">{toolList(tools.previous)}</Mono> : "None",
            ],
          ]}
        />
      )}
    </PanelCard>
  );
}

/** pinTitles names packages by their pinned title. */
const pinTitles = (status: Status | undefined) =>
  new Map((status?.agents?.pins ?? []).map((p) => [p.name, p.title]));

const toolList = (v: AgentVersion) =>
  Object.entries(v.packages ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([n, ver]) => `${n} ${ver}`)
    .join(", ");

function PickDialog({
  pin,
  onClose,
  onPicked,
}: {
  pin: Pin;
  onClose: () => void;
  onPicked: (version: string) => void;
}) {
  const versions = useQuery({
    queryKey: ["update-versions", pin.track, pin.name],
    queryFn: () =>
      verb<{ versions: string[] }>("update.versions", { name: pin.name, track: pin.track }),
    staleTime: 60_000,
  });
  const list = newestFirst(versions.data?.versions ?? []);
  const [version, setVersion] = useState<string | null>(null);
  const chosen = version !== null && list.includes(version) ? version : (list[0] ?? null);
  const error = versions.isError ? errorText(versions.error) : "";
  const id = useId();
  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Pick a version of {pin.title}</DialogTitle>
        </DialogHeader>
        <FormField
          id={id}
          label="Version"
          hint={`The image pins ${pin.image}; ${pin.current} runs now.`}
          error={error}
        >
          <Select value={chosen} onValueChange={(v) => setVersion(v)} disabled={!list.length}>
            <SelectTrigger {...fieldProps(id, error)} className="w-full font-mono text-xs">
              <SelectValue placeholder={versions.isFetching ? "Loading…" : ""} />
            </SelectTrigger>
            <SelectContent>
              {list.map((v) => (
                <SelectItem key={v} value={v} className="font-mono text-xs">
                  {v}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FormField>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            disabled={!chosen || chosen === pin.current}
            onClick={() => chosen && onPicked(chosen)}
          >
            Continue
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function History({ status }: { status: Status | undefined }) {
  const [dialog, confirm] = useConfirm();
  const versions = status?.agents?.versions ?? [];
  const titles = pinTitles(status);
  return (
    <PanelCard
      title="Agent set history"
      actions={
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            confirm({
              title: "Remove unused sets?",
              body: "Current, previous, any set in use, and the configured number of kept sets stay.",
              confirm: "Clean up",
              action: async () => {
                start("gc", {}, "Old versions removed.");
                return true;
              },
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
        <Table aria-label="Agent set history">
          <TableHeader>
            <TableRow>
              <TableHead>Status</TableHead>
              <TableHead>Changes</TableHead>
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
                changes={setChanges(v, setBefore(v, versions), titles)}
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

function VersionRows({ v, label, changes }: { v: AgentVersion; label: string; changes: string }) {
  const [open, setOpen] = useState(false);
  const detailId = useId();
  const pkgs = Object.keys(v.packages ?? {}).sort();
  return (
    <>
      <TableRow>
        <TableCell data-label="">
          <strong className="text-fg">{label}</strong>
        </TableCell>
        <TableCell data-label="Changes">
          <div className="flex flex-col gap-0.5">
            <span>{changes || "Same versions as before"}</span>
            <span className="font-mono text-xs text-fg-muted">{short(v.hash)}</span>
          </div>
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
