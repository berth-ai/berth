import { applyConfig } from "@/api/config";
import { keys, queryClient } from "@/api/queries";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";

/** BG ends the toast of a change agents pick up later. */
export const BG = " Agents pick up the change in the background.";

/** applyAndNotify applies a patch, says done or why not, and reloads the
 * config. It resolves whether the patch applied. */
export async function applyAndNotify(patch: object, done: string): Promise<boolean> {
  let ok = true;
  try {
    await applyConfig(patch);
    notify(done + BG);
  } catch (err) {
    notify(errorText(err), "bad");
    ok = false;
  }
  await queryClient.invalidateQueries({ queryKey: keys.config });
  return ok;
}
