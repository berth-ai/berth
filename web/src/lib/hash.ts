/** legacyHash maps M3 locations (#agents, #activity/12, #overview) to the
 * router's hash paths (#/agents, #/activity/12, #/), or returns null. */
export function legacyHash(hash: string): string | null {
  if (!hash || hash === "#" || hash.startsWith("#/")) return null;
  const path = hash.slice(1);
  return path === "overview" ? "#/" : `#/${path}`;
}

/** normalizeHash rewrites a legacy location in place, without a history entry. */
export function normalizeHash(): void {
  const next = legacyHash(window.location.hash);
  if (next)
    window.history.replaceState(null, "", window.location.pathname + window.location.search + next);
}
