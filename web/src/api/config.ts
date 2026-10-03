import { ApiError, verb } from "./request";
import type { ApplyResult, Config, ConfigView } from "./types";

/** applyConfig applies a patch and/or secrets on top of the latest revision. */
export async function applyConfig(
  patch: object | null,
  secrets?: Record<string, string | null>,
): Promise<ApplyResult> {
  const body: Record<string, unknown> = {};
  if (patch) {
    const cur = await verb<ConfigView>("config.get");
    body.base_revision = cur.config.revision;
    body.patch = patch;
  }
  if (secrets) body.secrets = secrets;
  return verb<ApplyResult>("config.apply", body);
}

/** conflictConfig is the server's current config carried by a 409 conflict. */
export function conflictConfig(err: unknown): Config | null {
  if (!(err instanceof ApiError) || err.status !== 409) return null;
  const body = err.body;
  if (typeof body === "object" && body !== null && "config" in body)
    return (body as { config: Config }).config;
  return null;
}
