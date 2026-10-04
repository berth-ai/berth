import { useState } from "react";
import { Segmented } from "@/components/segmented";
import { saveTheme, storedTheme, type Theme } from "@/lib/theme";

const THEMES: { value: Theme; label: string }[] = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
];

/** ThemePicker is the segmented Light / Dark / System control (DESIGN.md §4). */
export function ThemePicker() {
  const [theme, setTheme] = useState<Theme>(storedTheme);
  return (
    <Segmented
      label="Theme"
      value={theme}
      options={THEMES}
      size="sm"
      className="w-full"
      onChange={(next) => {
        setTheme(next);
        saveTheme(next);
      }}
    />
  );
}
