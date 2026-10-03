import type { ReactNode } from "react";

export function EmptyState({ text, action }: { text: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-3 p-6 text-center text-fg-secondary">
      <p>{text}</p>
      {action}
    </div>
  );
}
