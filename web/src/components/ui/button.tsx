import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const variants = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-lg border border-transparent bg-clip-padding text-sm font-medium whitespace-nowrap transition-all outline-none select-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-accent-hover",
        outline:
          "border-line-strong bg-surface text-fg hover:bg-surface-sunken hover:text-fg aria-expanded:bg-surface-sunken",
        secondary: "bg-secondary text-secondary-foreground hover:bg-line",
        ghost: "text-accent-text hover:text-accent-hover aria-expanded:bg-muted",
        destructive: "border-bad bg-bad text-bad-fg hover:opacity-90 focus-visible:ring-bad",
        link: "text-accent-text underline-offset-4 hover:underline",
      },
      size: {
        default:
          "h-8 gap-1.5 px-3 has-data-[icon=inline-end]:pr-2.5 has-data-[icon=inline-start]:pl-2.5",
        sm: "h-7 gap-1 px-2.5 text-xs [&_svg:not([class*='size-'])]:size-3.5",
        xs: "h-6 gap-1 px-2 text-xs [&_svg:not([class*='size-'])]:size-3",
        lg: "h-9 gap-1.5 px-4",
        icon: "size-8",
        "icon-xs": "size-6 [&_svg:not([class*='size-'])]:size-3",
        "icon-sm": "size-7",
        "icon-lg": "size-9",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

/** buttonVariants is a button's classes, merged so a variant's border and
 * colours win over the base's: a Link styled as a button needs this too. */
function buttonVariants({
  className,
  ...props
}: VariantProps<typeof variants> & { className?: string } = {}) {
  return cn(variants(props), className);
}

function Button({
  className,
  variant = "default",
  size = "default",
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof variants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      className={cn(variants({ variant, size, className }))}
      {...props}
    />
  );
}

export { Button, buttonVariants };
