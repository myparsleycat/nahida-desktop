import { cn } from "@renderer/lib/utils";
import * as React from "react";

// Groups settings under a quiet heading instead of an outlined container; a single left rule ties the rows together.
function Section({ className, ...props }: React.ComponentProps<"section">) {
  return (
    <section
      data-slot="section"
      className={cn("flex min-w-0 flex-col gap-1 text-sm", className)}
      {...props}
    />
  );
}

function SectionHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="section-header"
      className={cn(
        "grid auto-rows-min items-center gap-x-4 gap-y-0.5 px-1 pb-2 has-data-[slot=section-action]:grid-cols-[1fr_auto]",
        className,
      )}
      {...props}
    />
  );
}

function SectionTitle({ className, ...props }: React.ComponentProps<"h2">) {
  return (
    <h2
      data-slot="section-title"
      className={cn("text-sm font-medium text-muted-foreground", className)}
      {...props}
    />
  );
}

function SectionDescription({ className, ...props }: React.ComponentProps<"p">) {
  return (
    <p
      data-slot="section-description"
      className={cn("text-xs text-muted-foreground", className)}
      {...props}
    />
  );
}

function SectionAction({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="section-action"
      className={cn("col-start-2 row-span-2 row-start-1 justify-self-end", className)}
      {...props}
    />
  );
}

// The container owns the horizontal inset so self-padded children such as alerts line up with plain rows. Each direct
// child gets vertical padding with zero specificity, letting a row's own padding win without shifting its alignment.
// The flow layout is for free-form content such as step-by-step tools, where rows would be too loose.
function SectionContent({
  className,
  layout = "rows",
  ...props
}: React.ComponentProps<"div"> & { layout?: "rows" | "flow" }) {
  return (
    <div
      data-slot="section-content"
      data-layout={layout}
      className={cn(
        "flex flex-col border-l border-foreground/20 pr-3.5 pl-7.5",
        layout === "rows" ? "gap-1 [:where(&>*)]:py-3" : "gap-3 py-1",
        className,
      )}
      {...props}
    />
  );
}

// A setting row: title and description on the left, its control on the right. The text takes the remaining width so a
// long description wraps instead of squeezing the control. The row is a div, not a label, because controls such as
// buttons must not fire when the text is clicked; pass titleId and point the control's aria-labelledby at it.
function SectionRow({
  title,
  description,
  titleId,
  className,
  children,
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  title: React.ReactNode;
  description?: React.ReactNode;
  titleId?: string;
}) {
  return (
    <div
      data-slot="section-row"
      className={cn("flex items-center justify-between gap-6", className)}
      {...props}
    >
      <div className="min-w-0 flex-1 space-y-0.5">
        <span id={titleId} className="text-sm font-medium">
          {title}
        </span>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      {children}
    </div>
  );
}

export {
  Section,
  SectionAction,
  SectionContent,
  SectionDescription,
  SectionHeader,
  SectionRow,
  SectionTitle,
};
