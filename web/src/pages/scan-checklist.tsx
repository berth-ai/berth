import { useId } from "react";
import type { ScanRow } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { count } from "@/lib/format";
import { rowNote, selectable, shortCommit } from "./skills-logic";

/** ScanChecklist lists the skills a scan found at one commit. Installed and
 * broken skills show why and cannot be picked. */
export function ScanChecklist({
  rows,
  commit,
  picks,
  disabled,
  onChange,
}: {
  rows: ScanRow[];
  commit: string;
  picks: string[];
  disabled: boolean;
  onChange: (picks: string[]) => void;
}) {
  const head = useId();
  return (
    <div role="group" aria-labelledby={head} className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p id={head} className="font-medium">
          {count(rows.length, "skill", "skills")} at{" "}
          <code className="font-mono text-xs">{shortCommit(commit)}</code>
        </p>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={disabled}
            onClick={() => onChange(rows.filter(selectable).map((r) => r.path))}
          >
            Select all
          </Button>
          <Button variant="outline" size="sm" disabled={disabled} onClick={() => onChange([])}>
            Select none
          </Button>
        </div>
      </div>
      <ul aria-label="Skills found" className="flex max-h-72 flex-col overflow-y-auto">
        {rows.map((r) => {
          const note = rowNote(r);
          return (
            <li key={r.path} className="border-b border-line py-2 last:border-b-0">
              <Label className="flex items-start gap-2 font-normal leading-[inherit]">
                <Checkbox
                  className="mt-0.5"
                  checked={!note && picks.includes(r.path)}
                  disabled={disabled || Boolean(note)}
                  onCheckedChange={(on) =>
                    onChange(on ? [...picks, r.path] : picks.filter((p) => p !== r.path))
                  }
                />
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="font-mono text-xs font-semibold text-fg">
                    {r.name ?? r.path}
                  </span>
                  <span className="text-xs text-fg-muted">{note || r.description}</span>
                  <span className="font-mono text-xs break-all text-fg-muted">{r.path || "/"}</span>
                </span>
              </Label>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
