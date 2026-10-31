/** Light modules around the code, as the QR specification asks. */
const QUIET = 2;

/** QrCode draws a QR module matrix ("1" is dark) as one SVG path. It is
 * dark on light in both themes, so phone cameras read it. */
export function QrCode({ rows, label }: { rows: string[]; label: string }) {
  const size = rows.length + QUIET * 2;
  let d = "";
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) {
      if (row[x] === "1") d += `M${x + QUIET} ${y + QUIET}h1v1h-1z`;
    }
  });
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`0 0 ${size} ${size}`}
      shapeRendering="crispEdges"
      className="block aspect-square w-full max-w-60 rounded-md bg-log-fg text-log-bg"
    >
      <path d={d} fill="currentColor" />
    </svg>
  );
}
