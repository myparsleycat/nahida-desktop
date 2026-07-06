import { Input as InputPrimitive } from "@base-ui/react/input";
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
    <InputPrimitive
      type={type}
      data-slot="input"
      className={cn(
        "h-8 w-full min-w-0 rounded-lg border bg-transparent px-2.5 py-1 text-base transition-colors outline-none file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-input/50 disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30 dark:disabled:bg-input/80 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40",
        transparentBackground && "border-current/20",
        hideFocusRing
          ? cn(
              "focus-visible:ring-0",
              transparentBackground
                ? "focus-visible:border-current/20"
                : "focus-visible:border-input",
            )
          : transparentBackground
            ? "focus-visible:border-current/20 focus-visible:ring-3 focus-visible:ring-current/20"
            : "focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
        className,
      )}
      {...props}
    />
  );
}

export { Input };
