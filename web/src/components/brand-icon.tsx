import { cn } from "@/lib/utils";
import { BRAND_ICONS, iconFill, monogram } from "./brand-icons";

/** BrandIcon is a package's mark, or a monogram tile; decorative, since the
 * name is always shown beside it. */
export function BrandIcon({
  slug,
  name,
  id,
  size = 16,
}: {
  slug?: string;
  name: string;
  id: string;
  size?: 16 | 20;
}) {
  const icon = slug ? BRAND_ICONS[slug] : undefined;
  const box = size === 20 ? "size-5" : "size-4";
  if (icon) {
    return (
      <svg
        aria-hidden
        viewBox="0 0 24 24"
        className={cn(box, "shrink-0")}
        fill={iconFill(icon.hex)}
      >
        <path d={icon.path} />
      </svg>
    );
  }
  const m = monogram(name, id);
  return (
    <span
      aria-hidden
      className={cn(
        box,
        m.tint,
        "inline-flex shrink-0 items-center justify-center rounded-sm font-semibold",
        size === 20 ? "text-xs" : "text-2xs",
      )}
    >
      {m.letter}
    </span>
  );
}
