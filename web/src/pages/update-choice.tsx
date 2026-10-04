import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { count } from "@/lib/format";

/** UpdateChoice asks whether Update moves a skill's whole group (ids,
 * which include id) or the skill alone. */
export function UpdateChoice({
  id,
  ids,
  onPick,
  onClose,
}: {
  id: string;
  ids: string[];
  onPick: (ids: string[]) => void;
  onClose: () => void;
}) {
  return (
    <AlertDialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Update {id}?</AlertDialogTitle>
          <AlertDialogDescription>
            {id} came from the same repository as{" "}
            {count(ids.length - 1, "other skill", "other skills")}. Updating them together keeps
            them on one commit.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <Button variant="outline" onClick={() => onPick([id])}>
            Only {id}
          </Button>
          <Button onClick={() => onPick(ids)}>Update all {ids.length}</Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
