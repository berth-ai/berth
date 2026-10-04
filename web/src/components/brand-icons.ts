import { siClaude, siGooglegemini, siOpencode, siUpstash, type SimpleIcon } from "simple-icons";

/** BRAND_ICONS maps the lock's icon slugs to CC0 marks from simple-icons. */
export const BRAND_ICONS: Record<string, SimpleIcon> = {
  claude: siClaude,
  googlegemini: siGooglegemini,
  opencode: siOpencode,
  upstash: siUpstash,
};

/** SURFACES are --surface in the light and dark themes, as rrggbb; a test
 * keeps them in step with tokens.css. */
export const SURFACES = ["ffffff", "151c23"];

function luminance(hex: string): number {
  const v = parseInt(hex.slice(1), 16);
  const [r, g, b] = [(v >> 16) & 255, (v >> 8) & 255, v & 255].map((c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** contrast is the WCAG contrast ratio of two #rrggbb colours. */
export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/** iconFill keeps a brand colour that reads on both themes, else follows the text. */
export function iconFill(hex: string): string {
  const colour = `#${hex}`;
  return SURFACES.every((s) => contrast(colour, `#${s}`) >= 3) ? colour : "currentColor";
}

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
