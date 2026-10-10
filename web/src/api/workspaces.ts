import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { keys, queryClient } from "./queries";
import { verb } from "./request";

/** launchIn opens an agent, or a shell without one, in workspace id's
 * session. It toasts a failure and resolves whether it launched. */
export async function launchIn(id: string, agent?: string): Promise<boolean> {
  try {
    await verb("workspaces.launch", agent ? { id, agent } : { id });
  } catch (err) {
    notify(errorText(err, "The launch failed."), "bad");
    return false;
  }
  void queryClient.invalidateQueries({ queryKey: keys.workspaceStatus });
  return true;
}
