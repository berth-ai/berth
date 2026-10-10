import { Link } from "@tanstack/react-router";
import { useState } from "react";
import {
  refreshWorkspaceReport,
  useConfig,
  useStatus,
  useWorkspaceReport,
  useWorkspaceStatus,
} from "@/api/queries";
import { EmptyState } from "@/components/empty-state";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { OnOffSwitch } from "@/components/on-off-switch";
import { PanelCard } from "@/components/panel-card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { applyAndNotify } from "./apply-notify";
import { mcpFileNote, mcpNotes, mcpWorkspacesPatch, toggled } from "./attachments-logic";
import { type McpRow, mcpRows } from "./mcp-logic";

const mono = "font-mono text-xs";

/** WorkspaceMcp is a workspace's MCP tab (DESIGN.md §4): every server with
 * a switch for this workspace, the project files Berth writes and how git
 * sees them, and what each agent does with them. */
export function WorkspaceMcp({ id }: { id: string }) {
  const config = useConfig();
  const status = useStatus();
  const report = useWorkspaceReport(id);
  const states = useWorkspaceStatus();
  const packages = status.data?.packages;
  const view = config.data;
  if (!view || !packages) return <LoadingRows n={2} />;
  const rows = mcpRows(packages, view.config, view.secrets ?? {}).filter((r) => r.id);
  const lists = view.config.mcp?.workspaces ?? {};
  const st = states.data?.workspaces[id];
  const readOnly = Boolean(st?.exists && !st.writable);
  const mcp = report.data?.mcp;
  const files = Object.entries(mcp?.files ?? {}).sort(([a], [b]) => a.localeCompare(b));
  const skipped = Object.entries(mcp?.skipped ?? {});

  return (
    <>
      {readOnly ? (
        <InlineAlert tone="warn">
          Berth can't write here: skills, MCP and instructions can't be attached until agent can
          write this folder.
        </InlineAlert>
      ) : null}
      {mcp?.error ? <InlineAlert tone="bad">{mcp.error}</InlineAlert> : null}
      <PanelCard title="Servers">
        {rows.length ? (
          <Table aria-label="Servers in this workspace">
            <TableHeader>
              <TableRow>
                <TableHead>Server</TableHead>
                <TableHead>Transport</TableHead>
                <TableHead>In {id}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((r) => (
                <ServerRow key={r.id} id={id} row={r} list={lists[r.id]} disabled={readOnly} />
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyState
            text="No MCP servers yet."
            action={
              <Link to="/mcp" className="font-medium text-accent-text">
                Open MCP servers
              </Link>
            }
          />
        )}
      </PanelCard>
      {files.length || skipped.length ? (
        <PanelCard title="Files Berth writes">
          <ul aria-label="Project files" className="flex flex-col gap-2">
            {files.map(([path, f]) => (
              <li key={path} className="flex flex-col gap-0.5">
                <span className={cn(mono, "font-semibold text-fg")}>{path}</span>
                <span className={cn("text-xs", f.tracked ? "text-warn" : "text-fg-muted")}>
                  {mcpFileNote(f)} Servers: {(f.ids ?? []).join(", ")}.
                </span>
              </li>
            ))}
            {skipped.map(([path, why]) => (
              <li key={path} className="flex flex-col gap-0.5">
                <span className={cn(mono, "font-semibold text-fg")}>{path}</span>
                <span className="text-xs text-warn">{why}</span>
              </li>
            ))}
          </ul>
        </PanelCard>
      ) : null}
      <ul
        aria-label="How agents use workspace servers"
        className="flex flex-col gap-1 text-xs text-fg-muted"
      >
        {mcpNotes(packages, Boolean(mcp?.trust)).map((n) => (
          <li key={n}>{n}</li>
        ))}
        <li>
          Bearer tokens stay in Berth: the files name the token's variable, which agents read from
          their environment.
        </li>
      </ul>
    </>
  );
}

function ServerRow({
  id,
  row,
  list,
  disabled,
}: {
  id: string;
  row: McpRow;
  list: string[] | undefined;
  disabled: boolean;
}) {
  const on = (list ?? []).includes(id);
  const [pending, setPending] = useState<boolean | null>(null);
  const off = row.kind === "lock" ? !row.pkg.enabled : !row.server.enabled;
  const toggle = async (next: boolean) => {
    setPending(next);
    await applyAndNotify(
      mcpWorkspacesPatch(row.id, toggled(list, id, next)),
      `${row.name} is ${next ? "attached to" : "no longer in"} ${id}.`,
    );
    setPending(null);
    void refreshWorkspaceReport(id);
  };
  return (
    <TableRow>
      <TableCell data-label="">
        <div className="flex min-w-0 flex-col gap-0.5">
          <strong className="text-fg">{row.name}</strong>
          {off ? (
            <span className="text-xs text-fg-muted">
              Off everywhere; turn it on on the MCP page
            </span>
          ) : null}
        </div>
      </TableCell>
      <TableCell data-label="Transport">
        <span className={mono}>{row.kind === "lock" ? "stdio" : "HTTP"}</span>
      </TableCell>
      <TableCell data-label={`In ${id}`}>
        <OnOffSwitch
          checked={pending ?? on}
          disabled={disabled || pending !== null}
          label={`${on ? "Remove" : "Add"} ${row.name} ${on ? "from" : "to"} ${id}`}
          onCheckedChange={(next) => void toggle(next)}
        />
      </TableCell>
    </TableRow>
  );
}
