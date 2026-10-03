import { useId, useState } from "react";
import { useRegistry, useToolVersions } from "@/api/queries";
import { runJob } from "@/api/run-job";
import type { Job } from "@/api/types";
import { FormField, fieldProps } from "@/components/form-field";
import {
  Autocomplete,
  AutocompleteContent,
  AutocompleteInput,
  AutocompleteItem,
  AutocompleteList,
} from "@/components/ui/autocomplete";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { errorText } from "@/lib/errors";

/** ToolDialog adds a tool from the mise registry. It closes once the job has started. */
export function ToolDialog({
  onClose,
  onStarted,
}: {
  onClose: () => void;
  onStarted: (tool: string, version: string, job: Promise<Job | null>) => void;
}) {
  const [name, setName] = useState("");
  const [picked, setPicked] = useState("");
  const [version, setVersion] = useState<string | null>(null);
  const [versionError, setVersionError] = useState("");
  const registry = useRegistry(true);
  const versions = useToolVersions(picked);
  const list = [...(versions.data?.versions ?? [])].reverse();
  const chosen = version !== null && list.includes(version) ? version : (list[0] ?? null);
  const nameError = versions.isError ? errorText(versions.error) : "";
  const nameId = useId();
  const versionId = useId();

  const submit = () => {
    const t = name.trim();
    if (!t || !chosen) {
      setVersionError("Pick a tool and a version.");
      return;
    }
    onStarted(t, chosen, runJob("tools.add", { tool: t, version: chosen }));
    onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add a tool</DialogTitle>
        </DialogHeader>
        <FormField
          id={nameId}
          label="Tool"
          hint="A name from the mise registry, or backend:owner/repo."
          error={nameError}
        >
          <Autocomplete
            items={registry.data?.tools ?? []}
            value={name}
            onValueChange={(v: string) => {
              setName(v);
              if (registry.data?.tools.includes(v)) setPicked(v);
            }}
          >
            <AutocompleteInput
              {...fieldProps(nameId, nameError)}
              className="font-mono text-xs"
              spellCheck={false}
              autoComplete="off"
              onBlur={() => setPicked(name.trim())}
            />
            <AutocompleteContent>
              <AutocompleteList>
                {(item: string) => (
                  <AutocompleteItem key={item} value={item} className="font-mono text-xs">
                    {item}
                  </AutocompleteItem>
                )}
              </AutocompleteList>
            </AutocompleteContent>
          </Autocomplete>
        </FormField>
        <FormField id={versionId} label="Version" error={versionError}>
          <Select value={chosen} onValueChange={(v) => setVersion(v)} disabled={!list.length}>
            <SelectTrigger
              {...fieldProps(versionId, versionError)}
              className="w-full font-mono text-xs"
            >
              <SelectValue placeholder={versions.isFetching ? "Loading…" : ""} />
            </SelectTrigger>
            <SelectContent>
              {list.map((v) => (
                <SelectItem key={v} value={v} className="font-mono text-xs">
                  {v}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FormField>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={submit}>Add tool</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
