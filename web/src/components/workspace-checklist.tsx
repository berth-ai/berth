import type { ReactNode } from "react";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

/** WorkspaceChecklist picks workspaces for a skill or an MCP server
 * (DESIGN.md §4). A workspace another source already provides is locked
 * with takenText; extra renders beside a ticked workspace, such as the
 * Link / Vendor control. Nothing renders without workspaces. */
export function WorkspaceChecklist({
  workspaces,
  checked,
  taken = {},
  takenText = (other) => `From ${other}`,
  disabled,
  note,
  extra,
  onToggle,
}: {
  workspaces: string[];
  checked: string[];
  taken?: Record<string, string>;
  takenText?: (other: string) => string;
  disabled?: boolean;
  note?: ReactNode;
  extra?: (ws: string) => ReactNode;
  onToggle: (ws: string, on: boolean) => void;
}) {
  if (!workspaces.length) return null;
  return (
    <fieldset className="flex flex-col gap-2" disabled={disabled}>
      <legend className="mb-1 font-medium">Workspaces</legend>
      {workspaces.map((ws) => {
        const other = taken[ws];
        const on = checked.includes(ws);
        return (
          <div key={ws} className="flex flex-wrap items-center justify-between gap-2">
            <Label
              className={cn(
                "flex flex-wrap items-center gap-2 leading-[inherit] font-normal",
                other && "text-fg-muted",
              )}
            >
              <Checkbox
                checked={on}
                disabled={disabled || Boolean(other)}
                onCheckedChange={(c) => onToggle(ws, c)}
              />
              <span className="font-mono text-xs">{ws}</span>
              {other ? <span className="text-xs text-fg-muted">{takenText(other)}</span> : null}
            </Label>
            {on && extra ? extra(ws) : null}
          </div>
        );
      })}
      {note ? <p className="text-xs text-fg-muted">{note}</p> : null}
    </fieldset>
  );
}
