import { Copy } from "lucide-react";
import { type ComponentProps, Fragment, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { copyText } from "@/lib/copy";
import { cn } from "@/lib/utils";
import { OnOffSwitch } from "./on-off-switch";

const ERROR_LINE = /\b(error|failed|fatal)\b/i;

/** LogViewer shows a job log with Follow and Copy (DESIGN.md §4). */
export function LogViewer({ text, truncated }: { text: string; truncated: boolean }) {
  const [follow, setFollow] = useState(true);
  const pre = useRef<HTMLPreElement>(null);
  useEffect(() => {
    if (follow && pre.current) pre.current.scrollTop = pre.current.scrollHeight;
  }, [text, follow]);
  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="flex items-center justify-end gap-2 border-b px-3 py-2">
        <span className="text-fg-muted">Follow</span>
        <OnOffSwitch checked={follow} onCheckedChange={setFollow} label="Follow the log" />
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            void copyText(text, "Log copied.", "Copy failed; select the text instead.")
          }
        >
          <Copy aria-hidden />
          Copy
        </Button>
      </div>
      <LogText ref={pre} role="log" tabIndex={0} text={text} truncated={truncated} />
    </Card>
  );
}

/** LogText is log output on the dark log surface, error lines marked. */
export function LogText({
  text,
  truncated,
  className,
  ...props
}: { text: string; truncated?: boolean } & Omit<ComponentProps<"pre">, "children">) {
  const lines = text.replace(/\n$/, "").split("\n");
  return (
    <pre
      className={cn(
        "m-0 max-h-[60vh] overflow-auto bg-log-bg px-3 py-2.5 font-mono text-xs/[1.6] whitespace-pre text-log-fg",
        className,
      )}
      {...props}
    >
      {truncated ? <span className="text-log-dim">{"… earlier output truncated …\n"}</span> : null}
      {lines.map((line, i) => (
        <Fragment key={i}>
          {ERROR_LINE.test(line) ? <span className="text-log-err">{line}</span> : line}
          {"\n"}
        </Fragment>
      ))}
    </pre>
  );
}
