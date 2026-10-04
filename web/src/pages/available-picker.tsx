import { useState } from "react";
import type { AgentPackage } from "@/api/types";
import { BrandIcon } from "@/components/brand-icon";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { availableLabel, pickedAfter } from "./mcp-logic";

/** AgentChecklist picks the agents a server or skill reaches; null means all
 * agents. An agent with a note is reached anyway (shown ticked and locked);
 * unsupported agents are listed but cannot be picked. */
export function AgentChecklist({
  value,
  agents,
  disabled,
  notes = {},
  unsupported = [],
  onChange,
}: {
  value: string[] | null;
  agents: AgentPackage[];
  disabled?: boolean;
  notes?: Record<string, string>;
  unsupported?: AgentPackage[];
  onChange: (next: string[] | null) => void;
}) {
  const all = agents.map((a) => a.package);
  const ticked = new Set(value ?? all);
  return (
    <fieldset className="flex flex-col gap-2" disabled={disabled}>
      <legend className="mb-1 font-medium">Available to</legend>
      <label className="flex items-center gap-2">
        <Checkbox
          checked={value === null}
          disabled={disabled || value === null}
          onCheckedChange={(on) => (on ? onChange(null) : undefined)}
        />
        All agents
      </label>
      {agents.map((a) => {
        const note = notes[a.package];
        const on = ticked.has(a.package) || Boolean(note);
        return (
          <label key={a.package} className="flex items-center gap-2">
            <Checkbox
              checked={on}
              disabled={disabled || Boolean(note) || (on && ticked.size === 1)}
              onCheckedChange={(c) => onChange(pickedAfter(value, all, a.package, c))}
            />
            <BrandIcon slug={a.icon} name={a.name} id={a.package} />
            {a.name}
            {note ? <span className="text-xs text-fg-muted">{note}</span> : null}
          </label>
        );
      })}
      {unsupported.map((a) => (
        <label key={a.package} className="flex items-center gap-2 text-fg-muted">
          <Checkbox checked={false} disabled />
          <BrandIcon slug={a.icon} name={a.name} id={a.package} />
          {a.name}
          <span className="text-xs">Doesn't support skills</span>
        </label>
      ))}
    </fieldset>
  );
}

/** AvailablePicker shows who gets a server or skill and saves each change as
 * it is made. label replaces the default list of agents. */
export function AvailablePicker({
  server,
  value,
  agents,
  notes,
  unsupported,
  label,
  onChange,
}: {
  server: string;
  value: string[] | undefined;
  agents: AgentPackage[];
  notes?: Record<string, string>;
  unsupported?: AgentPackage[];
  label?: string;
  onChange: (next: string[] | null) => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const save = async (next: string[] | null) => {
    setBusy(true);
    await onChange(next);
    setBusy(false);
  };
  return (
    <Popover>
      <PopoverTrigger
        render={<Button variant="outline" size="sm" aria-label={`Choose agents for ${server}`} />}
      >
        {label ?? availableLabel(value, agents)}
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64">
        <AgentChecklist
          value={value ?? null}
          agents={agents}
          notes={notes}
          unsupported={unsupported}
          disabled={busy}
          onChange={(next) => void save(next)}
        />
      </PopoverContent>
    </Popover>
  );
}
