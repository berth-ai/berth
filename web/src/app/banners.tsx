import { Link } from "@tanstack/react-router";
import { CircleArrowUp, CircleX } from "lucide-react";
import { useState, useSyncExternalStore, type ReactNode } from "react";
import { getOnline, subscribeOnline } from "@/api/request";
import type { Status } from "@/api/types";
import { Button, buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { bannerKey, bannerText, dismissedCovers } from "@/pages/updates-logic";

export function failedCount(status: Status | undefined): number {
  return (status?.services ?? []).filter((s) => s.state === "failed").length;
}

/** updateBanner is the minor and patch updates to announce, or null; the
 * updates page shows them itself, and majors wait to be picked there. */
export function updateBanner(status: Status | undefined, pathname: string): string | null {
  return pathname === "/updates" ? null : bannerText(status?.check);
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

const DISMISSED = "berth.updates.dismissed";

function readDismissed(): string {
  try {
    return localStorage.getItem(DISMISSED) ?? "";
  } catch {
    return "";
  }
}

export function Banners({ status, pathname }: { status: Status | undefined; pathname: string }) {
  const online = useSyncExternalStore(subscribeOnline, getOnline);
  const [dismissed, setDismissed] = useState(readDismissed);
  const key = bannerKey(status?.check);
  const update = dismissedCovers(dismissed, key) ? null : updateBanner(status, pathname);
  const dismiss = () => {
    try {
      localStorage.setItem(DISMISSED, key);
    } catch {
      // Private windows may refuse storage; the banner hides for this visit.
    }
    setDismissed(key);
  };
  return (
    <>
      {online ? null : (
        <Banner tone="bad" title="Disconnected">
          The panel is not reachable. It may be restarting; this page retries on its own.
        </Banner>
      )}
      {update ? (
        <Banner
          tone="warn"
          title="Update available"
          action={
            <span className="flex gap-1">
              <Link to="/updates" className={buttonVariants({ variant: "outline", size: "sm" })}>
                Review
              </Link>
              {key ? (
                <Button
                  variant="ghost"
                  size="sm"
                  title="Hide until a newer version comes out"
                  onClick={dismiss}
                >
                  Dismiss
                </Button>
              ) : null}
            </span>
          }
        >
          {update}
        </Banner>
      ) : null}
    </>
  );
}
