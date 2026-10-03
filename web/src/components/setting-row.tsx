import { OnOffSwitch } from "./on-off-switch";

export function SettingRow({
  title,
  text,
  checked,
  onChange,
}: {
  title: string;
  text: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <div className="flex items-center gap-3">
      <div className="flex flex-1 flex-col gap-0.5">
        <strong>{title}</strong>
        <span className="text-fg-muted">{text}</span>
      </div>
      <OnOffSwitch checked={checked} onCheckedChange={onChange} label={title} />
    </div>
  );
}
