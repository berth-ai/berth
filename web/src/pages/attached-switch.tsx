import { useState } from "react";
import type { Attachment } from "@/api/types";
import { OnOffSwitch } from "@/components/on-off-switch";
import { enabledPatch } from "./sources-logic";

/** AttachedSwitch turns one attachment on or off, saving at once. */
export function AttachedSwitch({
  id,
  name,
  attachment,
  onApply,
}: {
  id: string;
  name: string;
  attachment: Attachment;
  onApply: (patch: object, done: string) => Promise<unknown>;
}) {
  const [pending, setPending] = useState<boolean | null>(null);
  const toggle = async (on: boolean) => {
    setPending(on);
    await onApply(enabledPatch(id, name, on), `${name}${on ? " is on." : " is off."}`);
    setPending(null);
  };
  return (
    <OnOffSwitch
      checked={pending ?? attachment.enabled}
      disabled={pending !== null}
      label={`${attachment.enabled ? "Turn off" : "Turn on"} ${name}`}
      onCheckedChange={(on) => void toggle(on)}
    />
  );
}
