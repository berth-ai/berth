import { cn } from "@/lib/utils";
import { BRAND_ICONS, monogram } from "./brand-icons";

/** BrandIcon is an agent's mark, or a monogram tile; decorative, since the
 * name is always shown beside it. */
export function BrandIcon({ slug, name, id }: { slug?: string; name: string; id: string }) {
  const icon = slug ? BRAND_ICONS[slug] : undefined;
  if (icon) {
    return (
      <svg
        aria-hidden
        viewBox="0 0 24 24"
        className="size-4 shrink-0"
        fill="currentColor"
        fillRule="evenodd"
      >
        <path d={icon} clipRule="evenodd" />
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
