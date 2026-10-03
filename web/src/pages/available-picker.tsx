import { useState } from "react";
import type { AgentPackage } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { availableLabel, pickedAfter } from "./mcp-logic";

/** AgentChecklist picks the agents a server reaches; null means all agents. */
export function AgentChecklist({
  value,
  agents,
  disabled,
  onChange,
}: {
  value: string[] | null;
  agents: AgentPackage[];
  disabled?: boolean;
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
        const on = ticked.has(a.package);
        return (
          <label key={a.package} className="flex items-center gap-2">
            <Checkbox
              checked={on}
              disabled={disabled || (on && ticked.size === 1)}
              onCheckedChange={(c) => onChange(pickedAfter(value, all, a.package, c))}
            />
            {a.name}
          </label>
        );
      })}
    </fieldset>
  );
}

/** AvailablePicker shows who gets a server and saves each change as it is made. */
export function AvailablePicker({
  server,
  value,
  agents,
  onChange,
}: {
  server: string;
  value: string[] | undefined;
  agents: AgentPackage[];
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
        {availableLabel(value, agents)}
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64">
        <AgentChecklist
          value={value ?? null}
          agents={agents}
          disabled={busy}
          onChange={(next) => void save(next)}
        />
      </PopoverContent>
    </Popover>
  );
}
