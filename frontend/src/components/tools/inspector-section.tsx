import { Collapsible } from "@base-ui/react/collapsible";
import { cn } from "@renderer/lib/utils";
import { ChevronRightIcon } from "lucide-react";
import { type ReactNode, useState } from "react";

// Open states outlive the tool window but not the app, so a section the user
// folded stays folded the next time the tool opens.
const openSections = new Map<string, boolean>();

/** A collapsible section of a tool panel that shows its summary while folded. */
export function InspectorSection({
  id,
  title,
  summary,
  actions,
  defaultOpen = false,
  className,
  headerClassName,
  panelClassName,
  children,
}: {
  id: string;
  title: ReactNode;
  summary?: ReactNode;
  /** Controls kept beside the header, outside the toggle. */
  actions?: ReactNode;
  defaultOpen?: boolean;
  className?: string;
  headerClassName?: string;
  panelClassName?: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(() => openSections.get(id) ?? defaultOpen);

  return (
    <Collapsible.Root
      open={open}
      onOpenChange={(next) => {
        openSections.set(id, next);
        setOpen(next);
      }}
      className={className}
    >
      <div className={cn("flex items-center gap-2", headerClassName)}>
        <Collapsible.Trigger className="group flex min-w-0 flex-1 items-center gap-1.5 rounded-sm text-left text-sm font-medium outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
          <ChevronRightIcon className="size-4 shrink-0 text-muted-foreground transition-transform group-data-panel-open:rotate-90" />
          <span className="truncate">{title}</span>
          {!open && summary !== undefined ? (
            <span className="ml-auto min-w-0 truncate pl-2 text-xs font-normal text-muted-foreground tabular-nums">
              {summary}
            </span>
          ) : null}
        </Collapsible.Trigger>
        {actions}
      </div>
      <Collapsible.Panel className={panelClassName}>{children}</Collapsible.Panel>
    </Collapsible.Root>
  );
}
