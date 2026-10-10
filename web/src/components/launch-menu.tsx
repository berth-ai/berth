import { ChevronDown, Play } from "lucide-react";
import type { AgentPackage } from "@/api/types";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

/** LaunchMenu opens an agent, or a shell, in a workspace's session
 * (DESIGN.md §4, Launch menu). onLaunch gets the agent's package, or
 * nothing for a shell. */
export function LaunchMenu({
  label,
  agents,
  disabled,
  onLaunch,
}: {
  label: string;
  agents: AgentPackage[];
  disabled?: boolean;
  onLaunch: (agent?: string) => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        render={<Button variant="outline" size="sm" aria-label={label} />}
      >
        <Play aria-hidden className="size-3.25" strokeWidth={1.6} />
        Launch
        <ChevronDown aria-hidden className="size-3.25" strokeWidth={1.6} />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {agents.map((a) => (
          <DropdownMenuItem key={a.package} onClick={() => onLaunch(a.package)}>
            {a.name}
          </DropdownMenuItem>
        ))}
        {agents.length ? <DropdownMenuSeparator /> : null}
        <DropdownMenuItem onClick={() => onLaunch()}>Shell</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
