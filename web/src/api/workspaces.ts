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

/** writeInstructions saves file in workspace id over the version base names;
 * it resolves the new sha256 and throws ApiError (changed_on_disk, …). */
export async function writeInstructions(
  id: string,
  file: string,
  content: string,
  base: string,
): Promise<string> {
  const r = await verb<{ sha256: string }>("workspaces.instructions.write", {
    id,
    file,
    content,
    base_sha256: base,
  });
  return r.sha256;
}

/** mergeInstructions appends file to AGENTS.md and replaces it with a link. */
export async function mergeInstructions(id: string, file: string): Promise<void> {
  await verb("workspaces.instructions.merge", { id, file });
}

/** restartSession restarts workspace id's session; it toasts a failure. */
export async function restartSession(id: string): Promise<boolean> {
  try {
    await verb("workspaces.restart", { id });
  } catch (err) {
    notify(errorText(err, "The restart failed."), "bad");
    return false;
  }
  void queryClient.invalidateQueries({ queryKey: keys.workspaceStatus });
  return true;
}
