import { notify } from "./notify";

export async function copyText(text: string, done: string, failed: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    notify(done);
  } catch {
    notify(failed, "bad");
  }
}
