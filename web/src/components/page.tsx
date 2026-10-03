import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export function Page({ narrow, children }: { narrow?: boolean; children: ReactNode }) {
  return (
    <div
      className={cn(
        "mx-auto flex w-full max-w-(--content-max) flex-col gap-5 p-[clamp(16px,3vw,32px)]",
        narrow && "max-w-[calc(var(--form-max)+64px)]",
      )}
    >
      {children}
    </div>
  );
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-end gap-3">
      <div className="flex min-w-0 flex-[1_1_280px] flex-col gap-0.5">
        <h1>{title}</h1>
        {description ? <p className="text-fg-muted">{description}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap gap-2">{actions}</div> : null}
    </header>
  );
}

export function SectionTitle({ children }: { children: ReactNode }) {
  return <h2 className="text-md">{children}</h2>;
}

export function CardGrid({ children }: { children: ReactNode }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(var(--card-min),1fr))] gap-3 max-sm:grid-cols-1">
      {children}
    </div>
  );
}
