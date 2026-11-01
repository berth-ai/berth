import { useState } from "react";
import { dayLabel, dayTitle, tokens } from "@/lib/format";
import { cn } from "@/lib/utils";

/** UsageBars charts tokens per day, oldest first, on one baseline. The
 * readout above names the hovered or focused day; with none, the total.
 * Every value is also in each bar's accessible name. */
export function UsageBars({
  days,
  total,
}: {
  days: { date: string; total: number }[];
  total: number;
}) {
  const [active, setActive] = useState<number | null>(null);
  const max = Math.max(1, ...days.map((d) => d.total));
  const shown = active === null ? null : days[active];
  return (
    <div className="flex flex-col gap-2">
      <p className="text-xs text-fg-muted tabular-nums" aria-hidden>
        {shown ? (
          <>
            <span className="text-fg">{dayTitle(shown.date)}</span> · {tokens(shown.total)} tokens
          </>
        ) : (
          <>
            <span className="text-fg">Last {days.length} days</span> · {tokens(total)} tokens
          </>
        )}
      </p>
      <ol
        aria-label="Tokens per day"
        className="m-0 grid h-24 list-none grid-flow-col auto-cols-fr items-end gap-1.5 border-b border-line p-0"
        onPointerLeave={() => setActive(null)}
      >
        {days.map((d, i) => (
          <li
            key={d.date}
            // The whole column is the hit target, not only the painted bar.
            className="flex h-full cursor-default items-end rounded-sm outline-offset-2 focus-visible:outline-2 focus-visible:outline-(--focus)"
            tabIndex={0}
            aria-label={`${dayTitle(d.date)}: ${d.total.toLocaleString()} tokens`}
            onPointerEnter={() => setActive(i)}
            onFocus={() => setActive(i)}
            onBlur={() => setActive(null)}
          >
            <span
              aria-hidden
              className={cn(
                "block w-full rounded-t-[4px] transition-colors",
                active === i ? "bg-accent-hover" : "bg-primary",
              )}
              // Any use shows at least 2 px; a day without any shows nothing.
              style={{ height: d.total ? `max(2px, ${(d.total / max) * 100}%)` : 0 }}
            />
          </li>
        ))}
      </ol>
      <div
        aria-hidden
        className="grid grid-flow-col auto-cols-fr gap-1.5 text-center text-xs text-fg-muted"
      >
        {days.map((d) => (
          <span key={d.date}>{dayLabel(d.date)}</span>
        ))}
      </div>
    </div>
  );
}

/** UsageSpark is UsageBars at a glance for a card: the same bars, small,
 * without labels or focus. It is decoration; the card states the numbers. */
export function UsageSpark({
  days,
  className,
}: {
  days: { date: string; total: number }[];
  className?: string;
}) {
  const max = Math.max(1, ...days.map((d) => d.total));
  return (
    <span
      aria-hidden
      className={cn(
        "grid h-8 w-24 grid-flow-col auto-cols-fr items-end gap-1 border-b border-line",
        className,
      )}
    >
      {days.map((d) => (
        <span
          key={d.date}
          className="block rounded-t-[2px] bg-primary"
          style={{ height: d.total ? `max(2px, ${(d.total / max) * 100}%)` : 0 }}
        />
      ))}
    </span>
  );
}
