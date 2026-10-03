import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";

/** BusyButton shows a spinner and a verb while busy and keeps its width:
 * both labels share one grid cell (DESIGN.md §4). */
export function BusyButton({
  busy,
  busyLabel,
  children,
  disabled,
  ...props
}: ComponentProps<typeof Button> & { busy: boolean; busyLabel: string }) {
  return (
    <Button {...props} disabled={disabled || busy} aria-busy={busy || undefined}>
      <span className="grid">
        <span
          className={cn(
            "col-start-1 row-start-1 inline-flex items-center justify-center gap-1.5",
            busy && "invisible",
          )}
        >
          {children}
        </span>
        {busy ? (
          <span className="col-start-1 row-start-1 inline-flex items-center justify-center gap-1.5">
            <Spinner className="size-3" />
            {busyLabel}
          </span>
        ) : null}
      </span>
    </Button>
  );
}
