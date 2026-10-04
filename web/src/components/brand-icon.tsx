import { useId } from "react";
import { cn } from "@/lib/utils";
import { BRAND_ICONS, monogram } from "./brand-icons";

// Older installed sets may predate icon metadata. Package names remain stable.
const AGENT_ICON_SLUGS: Record<string, string> = {
  "@anthropic-ai/claude-code": "claude",
  "@google/gemini-cli": "googlegemini",
  "@openai/codex": "codex",
  "opencode-ai": "opencode",
};

/** BrandIcon is an agent's mark, or a monogram tile; decorative, since the
 * name is always shown beside it. */
export function BrandIcon({ slug, name, id }: { slug?: string; name: string; id: string }) {
  // Gradient ids are document-wide, so each instance gets its own.
  const uid = useId();
  const iconSlug = slug || AGENT_ICON_SLUGS[id];
  const icon = iconSlug ? BRAND_ICONS[iconSlug] : undefined;
  if (icon) {
    const gradientId = (i: number) => `${uid}-g${i}`;
    return (
      <svg aria-hidden viewBox="0 0 24 24" className="size-4 shrink-0">
        {icon.gradients ? (
          <defs>
            {icon.gradients.map((g, i) => (
              <linearGradient
                key={i}
                id={gradientId(i)}
                gradientUnits="userSpaceOnUse"
                x1={g.x1}
                y1={g.y1}
                x2={g.x2}
                y2={g.y2}
              >
                {g.stops.map(([offset, colour]) => (
                  <stop key={offset} offset={offset} stopColor={colour} />
                ))}
              </linearGradient>
            ))}
          </defs>
        ) : null}
        {icon.paths.map((p) => (
          <path
            key={p.d}
            d={p.d}
            fill={typeof p.fill === "string" ? p.fill : `url(#${gradientId(p.fill.gradient)})`}
            fillRule={p.evenOdd ? "evenodd" : undefined}
          />
        ))}
      </svg>
    );
  }
  const m = monogram(name, id);
  return (
    <span
      aria-hidden
      className={cn(
        "size-4",
        m.tint,
        "inline-flex shrink-0 items-center justify-center rounded-sm font-semibold",
        "text-2xs",
      )}
    >
      {m.letter}
    </span>
  );
}
