import { useEffect } from "react";

/** useTitle sets the tab title to "<title> · Berth". */
export function useTitle(title: string): void {
  useEffect(() => {
    document.title = `${title} · Berth`;
  }, [title]);
}
