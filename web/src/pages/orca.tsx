import { Copy, Monitor, Smartphone } from "lucide-react";
import { useState } from "react";
import { refreshOrcaDevices, useConfig, useOrcaDevices, useStatus } from "@/api/queries";
import { verb } from "@/api/request";
import type { HostInfo, OrcaDevice, OrcaPair } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { EmptyState } from "@/components/empty-state";
import { Facts, Mono } from "@/components/facts";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { QrCode } from "@/components/qr-code";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { copyText } from "@/lib/copy";
import { errorText } from "@/lib/errors";
import { since } from "@/lib/format";
import { useTitle } from "@/lib/use-title";
import { FrontendOff } from "./frontend";

type Scope = OrcaPair["scope"];

const DEVICE: Record<Scope, string> = { desktop: "a desktop", mobile: "a phone" };

/** defaultAddress is what Orca apps dial without a pairing_address: the
 * host bind address and Orca port, or "" when the bind address is a
 * wildcard no client can dial. */
export function defaultAddress(host: HostInfo | null | undefined): string {
  const bind = host?.bind_address ?? "";
  if (!bind || bind === "0.0.0.0" || bind === "::") return "";
  return `${bind.includes(":") ? `[${bind}]` : bind}:${host?.orca_port || "6768"}`;
}

// Orca: pair the Orca desktop and mobile apps with Berth's headless Orca.
export function OrcaPage() {
  useTitle("Orca");
  const config = useConfig();
  const status = useStatus();
  const orca = config.data?.config.frontends.orca;
  const devices = useOrcaDevices(!!orca?.enabled);
  let body = <LoadingRows n={2} />;
  if (config.isError) body = <InlineAlert tone="bad">{errorText(config.error)}</InlineAlert>;
  else if (config.data)
    body = (
      <OrcaBody
        enabled={!!orca?.enabled}
        state={status.data?.services?.find((s) => s.name === "orca")?.state ?? "disabled"}
        address={orca?.pairing_address || defaultAddress(status.data?.host)}
        devices={devices.data}
        devicesError={devices.isError ? errorText(devices.error) : ""}
      />
    );
  return (
    <Page narrow>
      <PageHeader
        title="Orca"
        description="Drive Berth's agents and workspaces from the Orca desktop and mobile apps."
      />
      {body}
    </Page>
  );
}

export function OrcaBody({
  enabled,
  state,
  address,
  devices,
  devicesError = "",
}: {
  enabled: boolean;
  state: string;
  address: string;
  /** devices is undefined while loading. */
  devices?: OrcaDevice[];
  devicesError?: string;
}) {
  const [busy, setBusy] = useState<Scope | null>(null);
  const [result, setResult] = useState<OrcaPair | null>(null);
  const [error, setError] = useState("");
  if (!enabled) return <FrontendOff label="Orca server" />;
  const pair = async (scope: Scope) => {
    setBusy(scope);
    setError("");
    setResult(null);
    try {
      setResult(await verb<OrcaPair>("orca.pair", { scope }));
      void refreshOrcaDevices();
    } catch (err) {
      setError(errorText(err));
    }
    setBusy(null);
  };
  return (
    <>
      <PanelCard title="Server" badge={<StatusBadge state={state} />}>
        <Facts
          rows={[
            [
              "Pairing address",
              address ? (
                <Mono key="address">{address}</Mono>
              ) : (
                <span key="address" className="text-fg-muted">
                  Not set: set a pairing address in Settings, since the bind address is a wildcard.
                </span>
              ),
            ],
          ]}
        />
        <p className="text-fg-muted">Orca uses the agents and sign-ins of this Berth.</p>
      </PanelCard>
      <PairedDevices devices={devices} error={devicesError} />
      <PanelCard title="Pair a device">
        <p className="text-fg-secondary">
          Pairing restarts Orca once; your terminal sessions keep running.
        </p>
        <div className="flex flex-wrap gap-2">
          {(["desktop", "mobile"] as const).map((scope) => {
            const Icon = scope === "desktop" ? Monitor : Smartphone;
            return (
              <BusyButton
                key={scope}
                variant="outline"
                busy={busy === scope}
                busyLabel="Pairing…"
                disabled={busy !== null}
                onClick={() => void pair(scope)}
              >
                <Icon aria-hidden className="size-3.5" strokeWidth={1.6} />
                {scope === "desktop" ? "Pair a desktop" : "Pair a phone"}
              </BusyButton>
            );
          })}
        </div>
        <div aria-live="polite" className="flex flex-col gap-3">
          {error ? <InlineAlert tone="bad">{error}</InlineAlert> : null}
          {result ? <PairResult result={result} /> : null}
        </div>
      </PanelCard>
    </>
  );
}

function PairedDevices({ devices, error }: { devices?: OrcaDevice[]; error: string }) {
  let body = <LoadingRows n={2} />;
  if (error) body = <InlineAlert tone="bad">{error}</InlineAlert>;
  else if (devices && !devices.length) body = <EmptyState text="No devices are paired yet." />;
  else if (devices)
    body = (
      <ul
        aria-label="Paired devices"
        className="m-0 flex list-none flex-col divide-y rounded-md border p-0"
      >
        {devices.map((d) => {
          const Icon = d.scope === "mobile" ? Smartphone : Monitor;
          return (
            <li key={d.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
              <Icon aria-hidden className="size-4 text-fg-muted" strokeWidth={1.6} />
              <span className="flex min-w-0 flex-col">
                <span className="font-semibold wrap-anywhere">{d.name}</span>
                <span className="text-xs text-fg-muted">
                  {d.scope === "mobile" ? "Phone" : "Desktop"} · paired {day(d.paired_at)}
                </span>
              </span>
              <span className="flex-1" />
              <span className="text-xs text-fg-secondary tabular-nums">
                {d.last_seen_at ? `Last seen ${since(d.last_seen_at)} ago` : "Not connected yet"}
              </span>
            </li>
          );
        })}
      </ul>
    );
  return (
    <PanelCard title="Paired devices">
      {body}
      <p className="text-fg-muted">Remove a device in Orca, under Shared Server Access.</p>
    </PanelCard>
  );
}

function day(iso: string | null): string {
  return iso ? new Date(iso).toLocaleDateString() : "—";
}

function PairResult({ result }: { result: OrcaPair }) {
  const device = DEVICE[result.scope];
  return (
    <div className="flex flex-col gap-3 border-t pt-3">
      {result.warning ? <InlineAlert tone="warn">{result.warning}</InlineAlert> : null}
      <QrCode rows={result.qr} label={`Pairing QR code for ${device}`} />
      <p className="text-fg-secondary">
        {result.scope === "mobile"
          ? "Scan the code with Orca Mobile, or open the link on the phone."
          : "Open the link on the desktop, or paste it into Orca's Shared Server Access."}{" "}
        It dials <Mono>{result.address}</Mono>. A new link replaces any unused one.
      </p>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-md bg-surface-sunken px-2 py-1 font-mono text-xs">
          {result.url}
        </code>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void copyText(result.url, "Link copied.", "Could not copy the link.")}
        >
          <Copy aria-hidden className="size-3.25" strokeWidth={1.6} />
          Copy link
        </Button>
      </div>
    </div>
  );
}
