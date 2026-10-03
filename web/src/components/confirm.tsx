import { useRef, useState, type ReactNode } from "react";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { BusyButton } from "./busy-button";

export interface ConfirmSpec {
  title: string;
  body: ReactNode;
  confirm: string;
  danger?: boolean;
  action: () => Promise<boolean | void>;
}

/** useConfirm returns the dialog to render and ask(spec), which opens it.
 * The confirm button names the verb; only it may be danger. An action that
 * returns false keeps the dialog open; Escape and the scrim do nothing while
 * it runs (DESIGN.md §4). */
export function useConfirm(): [ReactNode, (spec: ConfirmSpec) => void] {
  const [spec, setSpec] = useState<ConfirmSpec | null>(null);
  const [busy, setBusy] = useState(false);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const run = async () => {
    if (!spec) return;
    setBusy(true);
    const keep = (await spec.action()) === false;
    setBusy(false);
    if (!keep) setSpec(null);
  };
  const dialog = (
    <AlertDialog
      open={spec !== null}
      onOpenChange={(open) => {
        if (!open && !busy) setSpec(null);
      }}
    >
      <AlertDialogContent initialFocus={confirmRef}>
        <AlertDialogHeader>
          <AlertDialogTitle>{spec?.title}</AlertDialogTitle>
          {typeof spec?.body === "string" ? (
            <AlertDialogDescription>{spec.body}</AlertDialogDescription>
          ) : null}
        </AlertDialogHeader>
        {typeof spec?.body === "string" ? null : spec?.body}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>Cancel</AlertDialogCancel>
          <BusyButton
            ref={confirmRef}
            variant={spec?.danger ? "destructive" : "default"}
            busy={busy}
            busyLabel={`${spec?.confirm ?? ""}…`}
            onClick={() => void run()}
          >
            {spec?.confirm}
          </BusyButton>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
  return [dialog, setSpec];
}
