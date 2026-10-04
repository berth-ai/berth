import type { ReactNode } from "react";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

/** PanelCard is the one card shell (DESIGN.md §4): title row with badge and
 * actions, a body, and an optional footer. */
export function PanelCard({
  title,
  badge,
  actions,
  footer,
  failed,
  className,
  children,
}: {
  title?: string;
  badge?: ReactNode;
  actions?: ReactNode;
  footer?: ReactNode;
  failed?: boolean;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <Card className={cn("min-w-0 gap-0 py-0", failed && "border-bad", className)}>
      {title ? (
        <CardHeader className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
          <CardTitle>{title}</CardTitle>
          {badge}
          <span className="flex-1" />
          {actions}
        </CardHeader>
      ) : null}
      {children ? (
        // A table runs edge to edge: its outer cells carry the 16 px inset (DESIGN.md §4).
        <CardContent className="flex flex-col gap-3 p-4 has-[>[data-slot=table-container]]:p-0">
          {children}
        </CardContent>
      ) : null}
      {footer ? (
        <CardFooter className="flex flex-wrap gap-2 border-t px-4 py-3">{footer}</CardFooter>
      ) : null}
    </Card>
  );
}
