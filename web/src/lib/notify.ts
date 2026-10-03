import { toast } from "@/components/ui/toast";

export type Tone = "ok" | "bad" | "warn" | "info";

const TYPES: Record<Tone, string> = { ok: "success", bad: "error", warn: "warning", info: "info" };

/** notify confirms briefly; errors stay until dismissed (DESIGN.md §4). */
export function notify(message: string, tone: Tone = "ok"): void {
  toast.add({ title: message, type: TYPES[tone], timeout: tone === "bad" ? 0 : 5000 });
}
