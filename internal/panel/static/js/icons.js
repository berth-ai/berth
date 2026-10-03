// Inline SVG icons (DESIGN.md §4): 16 × 16 viewBox, stroked in currentColor.
const NS = "http://www.w3.org/2000/svg";

const PATHS = {
  circle: "M8 1.5a6.5 6.5 0 1 0 0 13a6.5 6.5 0 1 0 0-13z",
  check: "M5 8.2l2 2 4-4.4",
  arrow: "M13.5 8a5.5 5.5 0 1 1-1.6-3.9M13.5 2v3h-3",
  up: "M8 11V5.5M5.8 7.5L8 5.3l2.2 2.2",
  clock: "M8 4.5V8l2.2 1.4",
  cross: "M5.5 5.5l5 5M10.5 5.5l-5 5",
  square: "M6 6h4v4H6z",
  dash: "M5 8h6",
  alert: "M8 4.8v3.8M8 10.8v.4",
  overview: "M2.5 2.5h4.5v4.5H2.5zM9 2.5h4.5v4.5H9zM2.5 9h4.5v4.5H2.5zM9 9h4.5v4.5H9z",
  terminal: "M2 3h12v10H2zM4.5 6.5l2 1.5-2 1.5M8 10h3",
  ide: "M6 4.5L2.5 8 6 11.5M10 4.5L13.5 8 10 11.5",
  agents: "M4 4.5h8v7H4zM6.5 7.5v1M9.5 7.5v1M8 2v2.5M2 8h2M12 8h2",
  tools: "M10.5 2.5a3 3 0 0 0-3 4l-5 5 1.5 1.5 5-5a3 3 0 0 0 4-3l-1.8 1.2-1.5-1.5z",
  accounts: "M8 8a2.75 2.75 0 1 0 0-5.5A2.75 2.75 0 0 0 8 8zM3 13.5c.6-2.4 2.6-3.8 5-3.8s4.4 1.4 5 3.8",
  settings: "M3 4.5h10M3 8h10M3 11.5h10M6 3v3M10 6.5v3M5 10v3",
  activity: "M2 8h3l2-4.5 2 9 2-4.5h3",
  external: "M9 2.5h4.5V7M13.5 2.5L7.5 8.5M11.5 9.5v4h-9v-9h4",
  back: "M10 3.5L5.5 8l4.5 4.5",
  copy: "M5.5 5.5h7v8h-7zM3.5 10.5v-8h7",
  menu: "M2.5 4.5h11M2.5 8h11M2.5 11.5h11",
  close: "M4 4l8 8M12 4l-8 8",
  plus: "M8 3v10M3 8h10",
};

/** icon draws one or more named paths. Decorative: hidden from assistive tech. */
export function icon(names, size = 16, stroke = 1.5) {
  const svg = document.createElementNS(NS, "svg");
  for (const [k, v] of Object.entries({
    viewBox: "0 0 16 16", width: size, height: size, fill: "none", stroke: "currentColor",
    "stroke-width": stroke, "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true",
  })) svg.setAttribute(k, String(v));
  for (const name of [].concat(names)) {
    const p = document.createElementNS(NS, "path");
    p.setAttribute("d", PATHS[name]);
    svg.append(p);
  }
  return svg;
}

/** statusIcon draws a §4 status icon: the circle with an inner mark, or the bare arrow. */
export function statusIcon(kind, size = 12) {
  return icon(kind === "arrow" ? ["arrow"] : ["circle", kind], size, 2);
}
