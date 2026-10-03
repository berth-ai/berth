import { useState } from "react";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { saveTheme, storedTheme, type Theme } from "@/lib/theme";

const THEMES: [Theme, string][] = [
  ["light", "Light"],
  ["dark", "Dark"],
  ["system", "System"],
];

/** ThemePicker is the segmented Light / Dark / System control (DESIGN.md §4). */
export function ThemePicker() {
  const [theme, setTheme] = useState<Theme>(storedTheme);
  return (
    <ToggleGroup
      aria-label="Theme"
      value={[theme]}
      onValueChange={(value) => {
        const next = THEMES.find(([t]) => t === value[0])?.[0];
        if (!next) return;
        setTheme(next);
        saveTheme(next);
      }}
      className="w-full gap-0.5 rounded-md bg-surface-sunken p-0.5"
    >
      {THEMES.map(([t, label]) => (
        <ToggleGroupItem
          key={t}
          value={t}
          className="h-6.5 flex-1 rounded-sm text-xs font-medium text-fg-muted data-pressed:bg-surface data-pressed:text-fg data-pressed:shadow-xs max-[900px]:h-8"
        >
          {label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}
