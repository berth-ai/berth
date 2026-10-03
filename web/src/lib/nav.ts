/** replaceLocation leaves the page; tests replace it to observe redirects. */
export function replaceLocation(url: string): void {
  window.location.replace(url);
}
