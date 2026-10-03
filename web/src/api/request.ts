// Talks to the panel API: same-origin JSON only. State-changing calls carry
// the header the server's CSRF check requires. A lost session goes back to
// sign-in, and an unfinished setup goes to the wizard.
import { replaceLocation } from "@/lib/nav";

export interface FieldProblem {
  field: string;
  message: string;
}

interface ErrorFields {
  code?: string;
  message?: string;
  fields?: FieldProblem[];
  retry_after?: number;
}

function errorOf(body: unknown): ErrorFields | undefined {
  if (typeof body !== "object" || body === null || !("error" in body)) return undefined;
  const e = body.error;
  return typeof e === "object" && e !== null ? (e as ErrorFields) : undefined;
}

/** ApiError is a failed panel call. status is 0 when the panel was not reachable. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly serverMessage: string;
  readonly fields: FieldProblem[];
  readonly retryAfter: number | undefined;
  readonly body: unknown;

  constructor(status: number, body: unknown) {
    const e = errorOf(body);
    super(e?.message || e?.code || `HTTP ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.code = e?.code ?? "";
    this.serverMessage = e?.message ?? "";
    this.fields = e?.fields ?? [];
    this.retryAfter = e?.retry_after;
    this.body = body;
  }
}

let online = true;
const watchers = new Set<() => void>();

function setOnline(value: boolean): void {
  if (online === value) return;
  online = value;
  for (const fn of watchers) fn();
}

/** subscribeOnline calls fn whenever the panel becomes reachable or not. */
export function subscribeOnline(fn: () => void): () => void {
  watchers.add(fn);
  return () => {
    watchers.delete(fn);
  };
}

export function getOnline(): boolean {
  return online;
}

const HEADERS = { "Content-Type": "application/json", "X-Berth-Panel": "1" };

/** request GETs path, or POSTs body as JSON, and returns the parsed answer. */
export async function request<T>(path: string, body?: unknown): Promise<T> {
  const init: RequestInit =
    body === undefined
      ? { cache: "no-store" }
      : { cache: "no-store", method: "POST", body: JSON.stringify(body), headers: HEADERS };
  let res: Response;
  try {
    res = await fetch(path, init);
  } catch {
    setOnline(false);
    throw new ApiError(0, { error: { code: "network", message: "The panel is not reachable." } });
  }
  setOnline(true);
  let data: unknown = null;
  try {
    data = await res.json();
  } catch {
    data = null;
  }
  if (res.ok) return data as T;
  const here = window.location.pathname;
  if (res.status === 401 && path !== "/api/login" && !here.startsWith("/login")) {
    replaceLocation("/login");
  } else if (res.status === 409 && errorOf(data)?.code === "setup_required" && here !== "/setup") {
    replaceLocation("/setup");
  }
  throw new ApiError(res.status, data);
}

export function get<T>(path: string): Promise<T> {
  return request<T>(path);
}

export function post<T>(path: string, body: unknown = {}): Promise<T> {
  return request<T>(path, body);
}

export function verb<T>(name: string, body: unknown = {}): Promise<T> {
  return post<T>(`/api/verbs/${name}`, body);
}

/** shouldRetry retries an unreachable panel or a server error twice, never a
 * 4xx: a 401 has already sent the browser to sign-in. */
export function shouldRetry(count: number, err: unknown): boolean {
  if (err instanceof ApiError && err.status >= 400 && err.status < 500) return false;
  return count < 2;
}
