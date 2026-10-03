import { CircleAlert } from "lucide-react";
import type { ReactNode } from "react";
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { cn } from "@/lib/utils";

/** fieldProps ties an input to its FormField's hint and error. */
export function fieldProps(id: string, error?: string) {
  return {
    id,
    "aria-describedby": `${id}-hint ${id}-err`,
    "aria-invalid": error ? true : undefined,
  } as const;
}

/** FormField: label above, hint below, error below the hint (DESIGN.md §4). */
export function FormField({
  id,
  label,
  hint,
  error,
  className,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <Field className={cn("min-w-0 gap-1.5", className)} data-invalid={error ? true : undefined}>
      <FieldLabel htmlFor={id} className="font-medium">
        {label}
      </FieldLabel>
      {children}
      {hint ? (
        <FieldDescription id={`${id}-hint`} className="text-xs">
          {hint}
        </FieldDescription>
      ) : null}
      {error ? (
        <FieldError id={`${id}-err`} className="flex items-center gap-1 text-xs text-bad">
          <CircleAlert aria-hidden className="size-3" strokeWidth={2} />
          {error}
        </FieldError>
      ) : null}
    </Field>
  );
}
