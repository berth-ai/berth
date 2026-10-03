import { ApiError } from "@/api/request";

/** errorText turns a failure into one sentence for the operator. */
export function errorText(err: unknown, fallback?: string): string {
  if (!(err instanceof ApiError) || (!err.code && !err.serverMessage)) {
    if (fallback) return fallback;
    return err instanceof ApiError && err.status
      ? `The panel answered ${err.status}.`
      : "The request failed.";
  }
  if (err.fields.length)
    return err.fields.map((f) => (f.field ? `${f.field}: ` : "") + f.message).join(" ");
  return err.serverMessage || err.code;
}

/** fieldErrors maps a failure's field errors to {field: message}. */
export function fieldErrors(err: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (err instanceof ApiError) for (const f of err.fields) out[f.field] = f.message;
  return out;
}
