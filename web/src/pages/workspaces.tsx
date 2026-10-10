import { Link, useNavigate } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useState } from "react";
import { useConfig, useStatus, useWorkspaceStatus } from "@/api/queries";
import type { WorkspaceState } from "@/api/types";
import { launchIn } from "@/api/workspaces";
import { EmptyState } from "@/components/empty-state";
import { LaunchMenu } from "@/components/launch-menu";
import { LoadingRows } from "@/components/loading";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
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
import { count } from "@/lib/format";
import { useTitle } from "@/lib/use-title";
import { AddWorkspaceDialog } from "./workspace-dialog";
import { gitSummary, launchAgents, workspaceRows } from "./workspaces-logic";

/** SessionBadge says, in words, whether a workspace's session runs. */
export function SessionBadge({ st }: { st: WorkspaceState | undefined }) {
  if (!st || st.unknown) return <StatusBadge state="unknown" small />;
  if (!st.session) return <span className="text-fg-muted">No session</span>;
  return (
    <StatusBadge
      state="running"
      detail={count(st.windows?.length ?? 0, "window", "windows")}
      small
    />
  );
}

export function WorkspacesPage() {
  useTitle("Workspaces");
  const config = useConfig();
  const status = useStatus();
  const states = useWorkspaceStatus();
  const navigate = useNavigate();
  const [adding, setAdding] = useState(false);
  const rows = config.data ? workspaceRows(config.data.config) : null;
  const agents = launchAgents(status.data?.packages ?? []);
  const open = (id: string, terminal = false) =>
    void navigate({
      to: "/workspaces/$id",
      params: { id },
      search: terminal ? { tab: "terminal" } : {},
    });
  return (
    <Page>
      <PageHeader
        title="Workspaces"
        description="Projects in /workspaces: launch agents in them and open their terminal."
        actions={
          <Button onClick={() => setAdding(true)}>
            <Plus aria-hidden /> Add workspace
          </Button>
        }
      />
      <PanelCard>
        {!rows ? (
          <LoadingRows n={2} />
        ) : rows.length ? (
          <Table aria-label="Workspaces">
            <TableHeader>
              <TableRow>
                <TableHead>Workspace</TableHead>
                <TableHead>Git</TableHead>
                <TableHead>Session</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map(({ id, ws }) => {
                const st = states.data?.workspaces[id];
                return (
                  <TableRow key={id}>
                    <TableCell>
                      <Link
                        to="/workspaces/$id"
                        params={{ id }}
                        className="font-semibold text-accent-text no-underline"
                      >
                        {id}
                      </Link>
                      <div className="font-mono text-xs text-fg-muted">{ws.path}</div>
                    </TableCell>
                    <TableCell className="text-fg-secondary">{gitSummary(st)}</TableCell>
                    <TableCell>
                      <SessionBadge st={st} />
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <LaunchMenu
                          label={`Launch in ${id}`}
                          agents={agents}
                          disabled={st?.exists === false}
                          onLaunch={(agent) =>
                            void launchIn(id, agent).then((ok) => ok && open(id, true))
                          }
                        />
                        <Button variant="outline" size="sm" onClick={() => open(id)}>
                          Manage
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        ) : (
          <EmptyState
            text="No workspaces yet. Clone a repository or adopt a folder in /workspaces."
            action={
              <Button variant="outline" onClick={() => setAdding(true)}>
                Add workspace
              </Button>
            }
          />
        )}
      </PanelCard>
      {adding ? (
        <AddWorkspaceDialog
          config={config.data?.config}
          onClose={() => setAdding(false)}
          onAdded={(id) => {
            setAdding(false);
            open(id);
          }}
        />
      ) : null}
    </Page>
  );
}
