import { Copy, ExternalLink } from "lucide-react";
import type { Device } from "@/api/types";
import { StatusBadge } from "@/components/status-badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { copyText } from "@/lib/copy";
import { deviceHost } from "@/pages/accounts-logic";

/** DeviceBox shows a device sign-in's code and the page to enter it on. */
export function DeviceBox({
  device,
  hint = "Enter this code on the code host:",
}: {
  device: Device;
  hint?: string;
}) {
  return (
    <div className="flex flex-col gap-2 rounded-md bg-info-soft p-4">
      <span className="text-fg-muted">{hint}</span>
      <span className="font-mono text-2xl font-semibold tracking-[.08em] wrap-anywhere text-fg">
        {device.code}
      </span>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            void copyText(device.code, "Code copied.", "Copy failed; type the code instead.")
          }
        >
          <Copy aria-hidden />
          Copy code
        </Button>
        <a
          href={device.url}
          target="_blank"
          rel="noopener noreferrer"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          <ExternalLink aria-hidden />
          Open {deviceHost(device.url)}
        </a>
      </div>
      <div className="flex items-center gap-2">
        <Spinner className="size-3" />
        <StatusBadge state="waiting" detail="for approval" />
      </div>
    </div>
  );
}
