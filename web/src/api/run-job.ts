import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { keys, queryClient } from "./queries";
import { ApiError, get, verb } from "./request";
import type { Job } from "./types";

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/** jobStartError says why a job verb did not start. */
export function jobStartError(err: unknown): string {
  return err instanceof ApiError && err.status === 409
    ? "Another job of this kind is running; wait for it to finish."
    : errorText(err, "The job did not start.");
}

/** runJob starts a job verb and resolves with the finished job, or null if
 * it did not start or vanished. It keeps polling after its page closes, so
 * the outcome's toast still appears (M3 behaviour). */
export async function runJob(
  name: string,
  body: unknown,
  onUpdate?: (job: Job) => void,
): Promise<Job | null> {
  let id: number;
  try {
    id = (await verb<{ job: Job }>(name, body)).job.id;
  } catch (err) {
    notify(jobStartError(err), "bad");
    return null;
  }
  void queryClient.invalidateQueries({ queryKey: keys.jobs });
  for (;;) {
    try {
      const { job } = await get<{ job: Job }>(`/api/jobs/${id}`);
      onUpdate?.(job);
      if (job.state !== "running") {
        void queryClient.invalidateQueries({ queryKey: keys.jobs });
        return job;
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) return null;
      // Otherwise keep polling: the panel may be restarting.
    }
    await sleep(1000);
  }
}
