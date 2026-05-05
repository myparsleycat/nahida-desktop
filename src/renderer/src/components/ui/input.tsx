import { cn } from "@renderer/lib/utils";
import * as React from "react";

type InputProps = React.ComponentProps<"input"> & {
  hideFocusRing?: boolean;
  transparentBackground?: boolean;
};

function Input({
  className,
  type,
  hideFocusRing = false,
  transparentBackground = false,
  ...props
}: InputProps) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "dark:bg-input/30 border-input aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive dark:aria-invalid:border-destructive/50 disabled:bg-input/50 dark:disabled:bg-input/80 h-8 rounded-lg border bg-transparent px-2.5 py-1 text-base transition-colors file:h-6 file:text-sm file:font-medium md:text-sm file:text-foreground placeholder:text-muted-foreground w-full min-w-0 outline-none file:inline-flex file:border-0 file:bg-transparent disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50",
        transparentBackground && "border-current/20",
        hideFocusRing
          ? cn(
              "focus-visible:ring-0",
              transparentBackground
                ? "focus-visible:border-current/20"
                : "focus-visible:border-input",
            )
          : transparentBackground
            ? "focus-visible:border-current/20 focus-visible:ring-[3px] focus-visible:ring-current/20"
            : "focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
        className,
      )}
      {...props}
    />
  );
}

export { Input };
