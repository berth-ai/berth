import { CircleAlert, CircleCheck, CircleX, Clock } from "lucide-react";
import type { ReactNode } from "react";
import type { Tone } from "@/lib/notify";
import { cn } from "@/lib/utils";

const ICONS = { ok: CircleCheck, bad: CircleX, warn: CircleAlert, info: Clock };
const LOOK: Record<Tone, string> = {
  ok: "bg-ok-soft [&>svg]:text-ok",
  bad: "bg-bad-soft [&>svg]:text-bad",
  warn: "bg-warn-soft [&>svg]:text-warn",
  info: "bg-info-soft [&>svg]:text-info",
};

/** InlineAlert is an icon, one sentence and an optional action. */
export function InlineAlert({
  tone,
  action,
  children,
}: {
  tone: Tone;
  action?: ReactNode;
  children: ReactNode;
}) {
  const Icon = ICONS[tone];
  return (
    <div
      role={tone === "bad" ? "alert" : "status"}
      className={cn("flex items-start gap-2 rounded-md px-3 py-2 text-fg", LOOK[tone])}
    >
      <Icon aria-hidden className="mt-px size-4 shrink-0" strokeWidth={2} />
      <span className="flex-1">{children}</span>
      {action}
    </div>
  );
}
