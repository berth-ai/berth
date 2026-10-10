import { Link, useNavigate } from "@tanstack/react-router";
import { ExternalLink } from "lucide-react";
import { useId, useState } from "react";
import { refreshWorkspaces, useConfig, useStatus, useWorkspaceStatus } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { Job, Workspace, WorkspaceState } from "@/api/types";
import { launchIn } from "@/api/workspaces";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { EmptyState } from "@/components/empty-state";
import { Facts, Mono } from "@/components/facts";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LaunchMenu } from "@/components/launch-menu";
import { LoadingRows } from "@/components/loading";
import { LogViewer } from "@/components/log-viewer";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { Segmented } from "@/components/segmented";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { FrontendOff, keepRightClick } from "./frontend";
import { repoLabel } from "./skills-logic";
import { SessionBadge } from "./workspaces";
import { WorkspaceMcp } from "./workspace-mcp";
import { WorkspaceSkills } from "./workspace-skills";
import {
  gitSummary,
  launchAgents,
  sessionName,
  terminalPath,
  type WorkspaceTab,
} from "./workspaces-logic";

export function WorkspacePage({
  id,
  tab,
  onTab,
}: {
  id: string;
  tab: WorkspaceTab;
  onTab: (t: WorkspaceTab) => void;
}) {
  useTitle(id);
  const config = useConfig();
  const status = useStatus();
  const states = useWorkspaceStatus();
  const navigate = useNavigate();
  const [confirm, ask] = useConfirm();
  const [deleting, setDeleting] = useState(false);
  const ws = config.data?.config.workspaces?.[id];
  const st = states.data?.workspaces[id];
  const toList = () => void navigate({ to: "/workspaces" });

  if (!config.data)
    return (
      <Page>
        <LoadingRows />
      </Page>
    );
  if (!ws)
    return (
      <Page>
        <PageHeader title="Workspace not found" />
        <PanelCard>
          <EmptyState
            text={`There is no workspace named ${id}.`}
            action={
              <Link to="/workspaces" className={buttonVariants({ variant: "outline" })}>
                Open Workspaces
              </Link>
            }
          />
        </PanelCard>
      </Page>
    );

  const forget = () =>
    ask({
      title: `Forget ${id}?`,
      body: `Berth stops its terminal session and forgets the workspace. The folder /workspaces/${ws.path} and its files stay; you can adopt it again. Berth also removes the skills, links and MCP entries it placed there; a vendored skill you edited stays.`,
      confirm: "Forget",
      danger: true,
      action: async () => {
        const done = await runJob("workspaces.forget", { id });
        if (done?.state !== "succeeded") {
          if (done)
            notify(
              done.result?.error?.message ?? "The job failed; open Activity for its log.",
              "bad",
            );
          return false;
        }
        notify(`${id} forgotten; its folder stays.`);
        toList();
        void refreshWorkspaces();
      },
    });

  return (
    <Page>
      <PageHeader
        title={id}
        description={`/workspaces/${ws.path}`}
        actions={
          <LaunchMenu
            label={`Launch in ${id}`}
            agents={launchAgents(status.data?.packages ?? [])}
            disabled={st?.exists === false}
            onLaunch={(agent) => void launchIn(id, agent).then((ok) => ok && onTab("terminal"))}
          />
        }
      />
      <Segmented
        label="Workspace view"
        value={tab}
        onChange={onTab}
        className="w-full max-w-[32rem]"
        options={[
          { value: "overview", label: "Overview" },
          { value: "terminal", label: "Terminal" },
          { value: "skills", label: "Skills" },
          { value: "mcp", label: "MCP" },
        ]}
      />
      {tab === "terminal" ? (
        <WorkspaceTerminal id={id} st={st} />
      ) : tab === "skills" ? (
        <WorkspaceSkills id={id} />
      ) : tab === "mcp" ? (
        <WorkspaceMcp id={id} />
      ) : (
        <Overview
          id={id}
          ws={ws}
          st={st}
          hostDir={status.data?.host?.workspaces_dir}
          onForget={forget}
          onDelete={() => setDeleting(true)}
        />
      )}
      {confirm}
      {deleting ? (
        <DeleteFolderDialog
          id={id}
          path={ws.path}
          onClose={() => setDeleting(false)}
          onDone={() => {
            setDeleting(false);
            toList();
          }}
        />
      ) : null}
    </Page>
  );
}

function Overview({
  id,
  ws,
  st,
  hostDir,
  onForget,
  onDelete,
}: {
  id: string;
  ws: Workspace;
  st: WorkspaceState | undefined;
  hostDir: string | undefined;
  onForget: () => void;
  onDelete: () => void;
}) {
  return (
    <>
      {st && !st.unknown && !st.exists ? (
        <InlineAlert tone="bad">
          The folder /workspaces/{ws.path} is missing. Restore it on the host, or forget the
          workspace.
        </InlineAlert>
      ) : null}
      {st?.exists && !st.writable ? (
        <InlineAlert tone="warn">
          Berth can't write here: skills, MCP and instructions can't be attached until agent can
          write /workspaces/{ws.path}.
        </InlineAlert>
      ) : null}
      <PanelCard title="Overview">
        <Facts
          rows={[
            ["Source", ws.repo ? <Mono key="source">{repoLabel(ws.repo)}</Mono> : "Adopted folder"],
            ["Folder", <Mono key="folder">/workspaces/{ws.path}</Mono>],
            [
              "On the host",
              <Mono key="host">{`${(hostDir ?? "./workspaces").replace(/\/$/, "")}/${ws.path}`}</Mono>,
            ],
            ["Git", gitSummary(st)],
            [
              "Session",
              <span key="session" className="inline-flex items-center gap-2">
                <Mono>{sessionName(id)}</Mono>
                <SessionBadge st={st} />
              </span>,
            ],
          ]}
        />
      </PanelCard>
      <PanelCard title="Remove">
        <p className="text-fg-secondary">
          Forget stops the session and removes the workspace from Berth; the folder stays. Delete
          folder also deletes /workspaces/{ws.path} and everything in it.
        </p>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" onClick={onForget}>
            Forget
          </Button>
          <Button variant="outline" onClick={onDelete}>
            Delete folder
          </Button>
        </div>
      </PanelCard>
    </>
  );
}

function WorkspaceTerminal({ id, st }: { id: string; st: WorkspaceState | undefined }) {
  const status = useStatus();
  const ttyd = status.data?.services?.find((s) => s.name === "ttyd");
  if (status.data && !ttyd) return <FrontendOff label="Terminal" />;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2 text-xs text-fg-muted">
        <span>
          Session <span className="font-mono">{sessionName(id)}</span>
        </span>
        <ul aria-label="Windows" className="flex flex-wrap gap-1.5">
          {(st?.windows ?? []).map((w, i) => (
            <li
              key={`${i}-${w}`}
              className="rounded-sm bg-surface-sunken px-2 py-0.5 font-mono text-fg-secondary"
            >
              {w}
            </li>
          ))}
        </ul>
        <a
          href={terminalPath(id)}
          target="_blank"
          rel="noopener"
          className={buttonVariants({ variant: "outline", size: "sm", className: "ml-auto" })}
        >
          <ExternalLink aria-hidden className="size-3.25" strokeWidth={1.6} />
          Open in new tab
        </a>
      </div>
      <div className="flex h-[70dvh] min-h-80 overflow-hidden rounded-lg border bg-log-bg">
        {status.data ? (
          <iframe
            key={id}
            title={`Terminal for ${id}`}
            src={terminalPath(id)}
            className="size-full flex-1 border-0"
            onLoad={(e) => keepRightClick(e.currentTarget)}
          />
        ) : null}
      </div>
    </div>
  );
}

/** DeleteFolderDialog forgets workspace id and deletes its folder once the
 * user has typed the folder's path. When agent cannot delete everything,
 * the workspace is already forgotten: it lists what is left and offers
 * only Done. */
export function DeleteFolderDialog({
  id,
  path,
  onClose,
  onDone,
}: {
  id: string;
  path: string;
  onClose: () => void;
  onDone: () => void;
}) {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [job, setJob] = useState<Job | null>(null);
  const inputId = useId();
  const error = job?.state === "failed" ? job.result?.error : undefined;
  const forgotten = error?.code === "delete_incomplete";
  const run = async () => {
    setBusy(true);
    const done = await runJob("workspaces.forget", { id, delete: true, confirm: typed }, setJob);
    setBusy(false);
    void refreshWorkspaces();
    if (done?.state === "succeeded") {
      notify(`${id} forgotten and its folder deleted.`);
      onDone();
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (open || busy) return;
        if (forgotten) onDone();
        else onClose();
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Delete the folder of {id}?</DialogTitle>
        </DialogHeader>
        <p className="text-fg-secondary">
          Berth stops the session, forgets {id} and deletes{" "}
          <span className="font-mono">/workspaces/{path}</span> with every file in it, uncommitted
          work included. This cannot be undone.
        </p>
        <FormField id={inputId} label={`Type ${path} to confirm`}>
          <Input
            {...fieldProps(inputId)}
            value={typed}
            className="font-mono text-xs"
            spellCheck={false}
            autoComplete="off"
            disabled={busy || forgotten}
            onChange={(e) => setTyped(e.target.value)}
          />
        </FormField>
        {error ? (
          <InlineAlert tone="bad">
            <div className="flex flex-col gap-1">
              <span>{error.message ?? "The job failed; open Activity for its log."}</span>
              {error.left?.length ? (
                <ul aria-label="Left behind" className="font-mono text-xs">
                  {error.left.map((p) => (
                    <li key={p}>{p}</li>
                  ))}
                </ul>
              ) : null}
            </div>
          </InlineAlert>
        ) : null}
        {job?.log ? <LogViewer text={job.log} truncated={job.log_truncated} /> : null}
        <DialogFooter>
          {forgotten ? (
            <Button onClick={onDone}>Done</Button>
          ) : (
            <>
              <Button variant="outline" disabled={busy} onClick={onClose}>
                Cancel
              </Button>
              <BusyButton
                variant="destructive"
                busy={busy}
                busyLabel="Deleting…"
                disabled={typed !== path}
                onClick={() => void run()}
              >
                Delete folder
              </BusyButton>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
