import type { ReactNode } from "react";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";

/** Segmented is the one segmented control (DESIGN.md §4): equal-width options
 * on a sunken track, the pressed one raised. "sm" is the nav's theme picker. */
export function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
  size = "default",
  className,
}: {
  label: string;
  value: T;
  options: readonly { value: T; label: ReactNode }[];
  onChange: (value: T) => void;
  size?: "default" | "sm";
  className?: string;
}) {
  return (
    <ToggleGroup
      aria-label={label}
      value={[value]}
      onValueChange={(next) => {
        const picked = options.find((o) => o.value === next[0]);
        if (picked) onChange(picked.value);
      }}
      className={cn("gap-0.5 rounded-md bg-surface-sunken p-0.5", className)}
    >
      {options.map((o) => (
        <ToggleGroupItem
          key={o.value}
          value={o.value}
          className={cn(
            "flex-1 rounded-sm font-medium text-fg-muted data-pressed:bg-surface data-pressed:text-fg data-pressed:shadow-xs",
            size === "sm" ? "h-6.5 text-xs max-[900px]:h-8" : "h-7 gap-1.5 px-3 max-sm:px-2",
          )}
        >
          {o.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}
