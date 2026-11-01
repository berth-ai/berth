import {
  CircleAlert,
  CircleArrowUp,
  CircleCheck,
  CircleMinus,
  CirclePlus,
  CircleStop,
  CircleX,
  Clock,
  RefreshCw,
  type LucideIcon,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

type BadgeTone = "ok" | "warn" | "info" | "bad" | "idle";

const TONES: Record<BadgeTone, string> = {
  ok: "bg-ok-soft text-ok",
  warn: "bg-warn-soft text-warn",
  info: "bg-info-soft text-info",
  bad: "bg-bad-soft text-bad",
  idle: "bg-idle-soft text-idle",
};

// Status vocabulary: tone, icon, word (DESIGN.md §4: never colour alone).
const STATES: Record<string, [BadgeTone, LucideIcon, string]> = {
  running: ["ok", CircleCheck, "Running"],
  succeeded: ["ok", CircleCheck, "Succeeded"],
  installed: ["ok", CircleCheck, "Installed"],
  signed_in: ["ok", CircleCheck, "Signed in"],
  set: ["ok", CircleCheck, "Set"],
  on: ["ok", CircleCheck, "On"],
  added: ["info", CirclePlus, "Added"],
  starting: ["warn", RefreshCw, "Starting"],
  restarting: ["warn", RefreshCw, "Restarting"],
  outdated: ["warn", CircleArrowUp, "Update available"],
  update: ["warn", CircleArrowUp, "Update available"],
  clash: ["warn", CircleAlert, "Clashes with a built-in server"],
  probe_failed: ["warn", CircleAlert, "Unknown"],
  in_progress: ["info", RefreshCw, "Running"],
  checking: ["info", RefreshCw, "Checking"],
  syncing: ["info", RefreshCw, "Syncing"],
  in_orca: ["ok", CircleCheck, "In Orca"],
  not_in_orca: ["idle", CircleMinus, "Not in Orca"],
  orca_failed: ["bad", CircleX, "Not added"],
  waiting: ["info", Clock, "Waiting"],
  failed: ["bad", CircleX, "Failed"],
  stopped: ["idle", CircleStop, "Stopped"],
  disabled: ["idle", CircleMinus, "Disabled"],
  off: ["idle", CircleMinus, "Off"],
  not_set: ["idle", CircleMinus, "Not set"],
  signed_out: ["idle", CircleMinus, "Signed out"],
  not_installed: ["idle", CircleMinus, "Not installed"],
  unknown: ["idle", CircleMinus, "Unknown"],
};

export function StatusBadge({
  state,
  detail,
  small,
}: {
  state: string;
  detail?: string | null;
  small?: boolean;
}) {
  const [tone, Icon, word] = STATES[state] ?? STATES.unknown;
  return (
    <Badge
      variant="outline"
      className={cn(
        "h-auto gap-1.25 rounded-sm border-transparent px-2 py-0.5 text-xs font-semibold",
        TONES[tone],
        small && "px-1.75 py-px text-2xs",
      )}
    >
      <Icon aria-hidden className={small ? "size-2.75" : "size-3"} strokeWidth={2} />
      {word}
      {detail ? <span className="font-normal text-fg-muted">{detail}</span> : null}
    </Badge>
  );
}
