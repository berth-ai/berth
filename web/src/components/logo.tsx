import { cn } from "@/lib/utils";

/** LogoMark is the dock outline: a box with no top edge (DESIGN.md §1). */
export function LogoMark({ large }: { large?: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "flex-none rounded-b-sm border-[2.5px] border-t-0 border-primary",
        large ? "h-3.25 w-5" : "h-3 w-4.5",
      )}
    />
  );
}
