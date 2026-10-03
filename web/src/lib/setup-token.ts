/** takeSetupToken reads the setup token from the URL fragment, which never
 * reaches a server or a log, and removes it from the address bar. Call it
 * before any request. A token in the query string is ignored and dropped. */
export function takeSetupToken(): string | null {
  const token = new URLSearchParams(window.location.hash.slice(1)).get("token");
  window.history.replaceState(null, "", "/setup");
  return token || null;
}

/** reloadOnSetupToken reloads the page when a token arrives in the fragment
 * later, as when a link is pasted into a tab already on /setup: only the
 * fragment changes, so the page would never read it. The reload runs
 * takeSetupToken again. Returns a function that stops listening. */
export function reloadOnSetupToken(reload = () => window.location.reload()): () => void {
  const onHash = () => {
    if (new URLSearchParams(window.location.hash.slice(1)).get("token")) reload();
  };
  window.addEventListener("hashchange", onHash);
  return () => window.removeEventListener("hashchange", onHash);
}
