import { ApiError } from "@/api/request";

export const USER = /^[A-Za-z0-9._@-]{1,64}$/;

export function strength(pw: string): string {
  if (pw.length < 10) return "Too short: use at least 10 characters.";
  const kinds = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^A-Za-z0-9]/].filter((r) => r.test(pw)).length;
  if (pw.length >= 16 || kinds >= 3) return "Strong.";
  return "Acceptable. A longer passphrase is stronger.";
}

/** lockSeconds is how long sign-in stays locked after err, or 0. */
export function lockSeconds(err: unknown): number {
  if (!(err instanceof ApiError)) return 0;
  const retry = err.retryAfter ?? 0;
  return err.status === 429 || retry > 2 ? retry || 1 : 0;
}
