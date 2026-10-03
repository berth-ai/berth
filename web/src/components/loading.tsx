import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

export function LoadingRows({ n = 3 }: { n?: number }) {
  return (
    <div className="flex flex-col gap-2" aria-busy="true">
      {Array.from({ length: n }, (_, i) => (
        <Skeleton key={i} className={cn("h-3.5", i % 2 === 1 && "w-[70%]")} />
      ))}
    </div>
  );
}
