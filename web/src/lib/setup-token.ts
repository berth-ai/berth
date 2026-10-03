/** takeSetupToken reads the setup token from the URL fragment, which never
 * reaches a server or a log, and removes it from the address bar. Call it
 * before any request. A token in the query string is ignored and dropped. */
export function takeSetupToken(): string | null {
  const token = new URLSearchParams(window.location.hash.slice(1)).get("token");
  window.history.replaceState(null, "", "/setup");
  return token || null;
}
