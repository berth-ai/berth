import { Link } from "@tanstack/react-router";
import { CircleArrowUp, CircleX } from "lucide-react";
import { useSyncExternalStore, type ReactNode } from "react";
import { getOnline, subscribeOnline } from "@/api/request";
import type { Status, UpdateCheck } from "@/api/types";
import { buttonVariants } from "@/components/ui/button";
import { short } from "@/lib/format";
import { cn } from "@/lib/utils";

export function failedCount(status: Status | undefined): number {
  return (status?.services ?? []).filter((s) => s.state === "failed").length;
}

/** updateBanner is the check to announce, or null; the agents page shows it itself. */
export function updateBanner(status: Status | undefined, pathname: string): UpdateCheck | null {
  const check = status?.check;
  return check?.update_available && pathname !== "/agents" ? check : null;
}

function Banner({
  tone,
  title,
  action,
  children,
}: {
  tone: "bad" | "warn";
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  const Icon = tone === "bad" ? CircleX : CircleArrowUp;
  return (
    <div
      role={tone === "bad" ? "alert" : "status"}
      className={cn(
        "flex flex-wrap items-center gap-2 border-b px-[clamp(16px,3vw,32px)] py-2.5 text-fg-secondary",
        tone === "bad"
          ? "bg-bad-soft [&>strong]:text-bad [&>svg]:text-bad"
          : "bg-warn-soft [&>strong]:text-warn [&>svg]:text-warn",
      )}
    >
      <Icon aria-hidden className="size-4" strokeWidth={2} />
      <strong className="font-semibold">{title}</strong>
      <span>{children}</span>
      {action}
    </div>
  );
}

export function Banners({ status, pathname }: { status: Status | undefined; pathname: string }) {
  const online = useSyncExternalStore(subscribeOnline, getOnline);
  const check = updateBanner(status, pathname);
  return (
    <>
      {online ? null : (
        <Banner tone="bad" title="Disconnected">
          The panel is not reachable. It may be restarting; this page retries on its own.
        </Banner>
      )}
      {check ? (
        <Banner
          tone="warn"
          title="Update available"
          action={
            <Link to="/agents" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Review
            </Link>
          }
        >
          A newer agent set is on {check.ref} ({short(check.hash)}).
        </Banner>
      ) : null}
    </>
  );
}
