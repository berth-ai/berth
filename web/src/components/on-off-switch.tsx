import { Switch } from "@/components/ui/switch";

/** OnOffSwitch is a role=switch with its On/Off word to the left, so the
 * state reads without colour (DESIGN.md §4). word replaces On/Off, as in a
 * group switch's "2 of 3". */
export function OnOffSwitch({
  checked,
  onCheckedChange,
  label,
  disabled,
  word,
}: {
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
  label: string;
  disabled?: boolean;
  word?: string;
}) {
  return (
    <span className="inline-flex items-center gap-2">
      <span className="min-w-5.5 text-right font-medium text-fg-secondary">
        {word ?? (checked ? "On" : "Off")}
      </span>
      <Switch
        checked={checked}
        onCheckedChange={(v) => onCheckedChange(v)}
        aria-label={label}
        disabled={disabled}
      />
    </span>
  );
}
