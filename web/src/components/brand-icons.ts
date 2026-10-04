/*! Paths from @lobehub/icons-static-svg 1.95.1, icons/claudecode-color.svg,
 * geminicli-color.svg, codex-color.svg and opencode.svg (which has no colour
 * variant). https://github.com/lobehub/lobe-icons
 * Package tarball SHA-256: fc2a063f9c5db18f6b3f27a62670c95a9e767cc45ba7bbe5d949f9f9af090fc3
 *
 * License: public/licenses/lobe-icons.txt (included in the built panel).
 */

/** A MarkPath fills with a colour, currentColor, or one of the mark's gradients. */
export interface MarkPath {
  d: string;
  fill: string | { gradient: number };
  evenOdd?: true;
}

/** A MarkGradient is a linearGradient in the 24 × 24 viewBox's user space. */
export interface MarkGradient {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  stops: [offset: number, colour: string][];
}

export interface BrandMark {
  paths: MarkPath[];
  gradients?: MarkGradient[];
}

/** BRAND_ICONS maps the lock's icon slugs to LobeHub marks in their own colours. */
export const BRAND_ICONS: Record<string, BrandMark> = {
  claude: {
    paths: [
      {
        d: "M20.998 10.949H24v3.102h-3v3.028h-1.487V20H18v-2.921h-1.487V20H15v-2.921H9V20H7.488v-2.921H6V20H4.487v-2.921H3V14.05H0V10.95h3V5h17.998v5.949zM6 10.949h1.488V8.102H6v2.847zm10.51 0H18V8.102h-1.49v2.847z",
        fill: "#D97757",
        evenOdd: true,
      },
    ],
  },
  googlegemini: {
    paths: [
      {
        d: "M0 4.391A4.391 4.391 0 014.391 0h15.217A4.391 4.391 0 0124 4.391v15.217A4.391 4.391 0 0119.608 24H4.391A4.391 4.391 0 010 19.608V4.391z",
        fill: { gradient: 0 },
      },
      {
        d: "M19.74 1.444a2.816 2.816 0 012.816 2.816v15.48a2.816 2.816 0 01-2.816 2.816H4.26a2.816 2.816 0 01-2.816-2.816V4.26A2.816 2.816 0 014.26 1.444h15.48zM7.236 8.564l7.752 3.728-7.752 3.727v2.802l9.557-4.596v-3.866L7.236 5.763v2.801z",
        fill: "#1E1E2E",
        evenOdd: true,
      },
    ],
    gradients: [
      {
        x1: 24,
        y1: 6.587,
        x2: 0,
        y2: 16.494,
        stops: [
          [0, "#EE4D5D"],
          [0.328, "#B381DD"],
          [0.476, "#207CFE"],
        ],
      },
    ],
  },
  codex: {
    paths: [
      {
        d: "M19.503 0H4.496A4.496 4.496 0 000 4.496v15.007A4.496 4.496 0 004.496 24h15.007A4.496 4.496 0 0024 19.503V4.496A4.496 4.496 0 0019.503 0z",
        fill: "#fff",
      },
      {
        d: "M9.064 3.344a4.578 4.578 0 012.285-.312c1 .115 1.891.54 2.673 1.275.01.01.024.017.037.021a.09.09 0 00.043 0 4.55 4.55 0 013.046.275l.047.022.116.057a4.581 4.581 0 012.188 2.399c.209.51.313 1.041.315 1.595a4.24 4.24 0 01-.134 1.223.123.123 0 00.03.115c.594.607.988 1.33 1.183 2.17.289 1.425-.007 2.71-.887 3.854l-.136.166a4.548 4.548 0 01-2.201 1.388.123.123 0 00-.081.076c-.191.551-.383 1.023-.74 1.494-.9 1.187-2.222 1.846-3.711 1.838-1.187-.006-2.239-.44-3.157-1.302a.107.107 0 00-.105-.024c-.388.125-.78.143-1.204.138a4.441 4.441 0 01-1.945-.466 4.544 4.544 0 01-1.61-1.335c-.152-.202-.303-.392-.414-.617a5.81 5.81 0 01-.37-.961 4.582 4.582 0 01-.014-2.298.124.124 0 00.006-.056.085.085 0 00-.027-.048 4.467 4.467 0 01-1.034-1.651 3.896 3.896 0 01-.251-1.192 5.189 5.189 0 01.141-1.6c.337-1.112.982-1.985 1.933-2.618.212-.141.413-.251.601-.33.215-.089.43-.164.646-.227a.098.098 0 00.065-.066 4.51 4.51 0 01.829-1.615 4.535 4.535 0 011.837-1.388zm3.482 10.565a.637.637 0 000 1.272h3.636a.637.637 0 100-1.272h-3.636zM8.462 9.23a.637.637 0 00-1.106.631l1.272 2.224-1.266 2.136a.636.636 0 101.095.649l1.454-2.455a.636.636 0 00.005-.64L8.462 9.23z",
        fill: { gradient: 0 },
      },
    ],
    gradients: [
      {
        x1: 12,
        y1: 3,
        x2: 12,
        y2: 21,
        stops: [
          [0, "#B1A7FF"],
          [0.5, "#7A9DFF"],
          [1, "#3941FF"],
        ],
      },
    ],
  },
  opencode: {
    paths: [{ d: "M16 6H8v12h8V6zm4 16H4V2h16v20z", fill: "currentColor", evenOdd: true }],
  },
};

// Soft tints from DESIGN.md; text-primary is the accent colour.
const TINTS = [
  "bg-accent-soft text-primary",
  "bg-info-soft text-info",
  "bg-ok-soft text-ok",
  "bg-warn-soft text-warn",
  "bg-idle-soft text-idle",
];

/** monogram is a name's first letter or digit on a tint picked by key. */
export function monogram(name: string, key: string): { letter: string; tint: string } {
  let h = 0;
  for (const ch of key) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  const letter = (name.match(/[\p{L}\p{N}]/u)?.[0] ?? "?").toUpperCase();
  return { letter, tint: TINTS[h % TINTS.length] };
}
