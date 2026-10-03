import { Fragment, type ReactNode } from "react";

export function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <dl className="m-0 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1">
      {rows.map(([label, value]) => (
        <Fragment key={label}>
          <dt className="text-fg-muted">{label}</dt>
          <dd className="m-0 wrap-anywhere tabular-nums">{value}</dd>
        </Fragment>
      ))}
    </dl>
  );
}

export function Mono({ children }: { children: ReactNode }) {
  return <span className="font-mono text-xs">{children}</span>;
}
