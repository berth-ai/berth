import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { notify } from "@/lib/notify";
import { BusyButton } from "./busy-button";
import { useConfirm } from "./confirm";
import { FormField, fieldProps } from "./form-field";
import { StatusBadge } from "./status-badge";

/** SecretField is write-only: once saved, a key is only ever "Set". */
export function SecretField({
  name,
  isSet,
  onSave,
  onRemove,
}: {
  name: string;
  isSet: boolean;
  onSave: (value: string) => Promise<string | null>;
  onRemove: () => Promise<string | null>;
}) {
  const [editing, setEditing] = useState(!isSet);
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [dialog, confirm] = useConfirm();
  const id = useId();

  if (!editing) {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs">{name}</span>
        <StatusBadge state="set" />
        <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
          Replace
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            confirm({
              title: `Remove ${name}?`,
              body: "Agents that read this key stop working with it until you set it again.",
              confirm: "Remove key",
              danger: true,
              action: async () => {
                const err = await onRemove();
                if (err) {
                  notify(err, "bad");
                  return false;
                }
                setEditing(true);
                return true;
              },
            })
          }
        >
          Remove
        </Button>
        {dialog}
      </div>
    );
  }

  const save = async () => {
    if (!value) {
      setError("Paste a key first.");
      return;
    }
    setBusy(true);
    const err = await onSave(value);
    setBusy(false);
    if (err) {
      setError(err);
      return;
    }
    setValue("");
    setError("");
    setEditing(false);
  };

  return (
    <div className="flex w-full items-end gap-2">
      <FormField id={id} label={name} error={error} className="flex-1">
        <Input
          {...fieldProps(id, error)}
          type="password"
          autoComplete="off"
          spellCheck={false}
          placeholder="Paste the key"
          className="font-mono text-xs"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
      </FormField>
      <BusyButton size="sm" busy={busy} busyLabel="Saving…" onClick={() => void save()}>
        Save
      </BusyButton>
    </div>
  );
}
