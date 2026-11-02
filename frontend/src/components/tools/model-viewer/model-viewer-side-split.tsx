import { cn } from "@renderer/lib/utils";
import { ChevronLeftIcon } from "lucide-react";
import { type CSSProperties, type ReactNode, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

const DEFAULT_SIDE_WIDTH = 360;
const MIN_SIDE_WIDTH = 280;
// The viewer's minimum width plus the bar. The grid template repeats this as 332px.
const RESERVED_WIDTH = 332;
const DRAG_THRESHOLD = 4;
const KEYBOARD_STEP = 16;

/**
 * Owns the width and open state so that dragging rerenders only this component:
 * `viewer` and `side` keep their element identity and React skips them.
 */
export function ModelViewerSideSplit({
  viewer,
  side,
  resizable,
}: {
  viewer: ReactNode;
  side: ReactNode;
  resizable: boolean;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(true);
  const [width, setWidth] = useState(DEFAULT_SIDE_WIDTH);
  const [maxWidth, setMaxWidth] = useState<number>();
  const gridRef = useRef<HTMLDivElement>(null);
  const sideRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{
    pointerId: number;
    startX: number;
    startWidth: number;
    moved: boolean;
  } | null>(null);

  const measureMaxWidth = () =>
    Math.max(
      MIN_SIDE_WIDTH,
      (gridRef.current?.getBoundingClientRect().width ?? 0) - RESERVED_WIDTH,
    );
  // The rendered width can be below the state while the template's min() applies to a narrow window.
  const renderedWidth = () => (open ? (sideRef.current?.getBoundingClientRect().width ?? 0) : 0);
  const resize = (next: number) => {
    const max = measureMaxWidth();
    setMaxWidth(max);
    setWidth(Math.max(MIN_SIDE_WIDTH, Math.min(next, max)));
    setOpen(true);
  };

  return (
    <div
      ref={gridRef}
      className={cn(
        "grid min-h-0 flex-1 gap-3",
        resizable
          ? cn(
              "lg:gap-0",
              open
                ? "lg:grid-cols-[minmax(0,1fr)_auto_min(var(--model-viewer-side-width),calc(100%-332px))]"
                : "lg:grid-cols-[minmax(0,1fr)_auto]",
            )
          : side && "lg:grid-cols-[minmax(0,1fr)_360px]",
      )}
      style={
        resizable ? ({ "--model-viewer-side-width": `${width}px` } as CSSProperties) : undefined
      }
    >
      {viewer}
      {resizable ? (
        <div
          role="separator"
          tabIndex={0}
          title={t("page.tools.model_viewer.toggle_viewer_splitter")}
          aria-label={t("page.tools.model_viewer.toggle_viewer_splitter")}
          aria-orientation="vertical"
          aria-valuemin={0}
          aria-valuemax={maxWidth}
          aria-valuenow={open ? width : 0}
          className="group hidden w-3 cursor-col-resize touch-none items-center justify-center outline-none select-none lg:flex"
          onFocus={() => setMaxWidth(measureMaxWidth())}
          onPointerDown={(event) => {
            if (event.button !== 0) return;
            event.currentTarget.setPointerCapture(event.pointerId);
            dragRef.current = {
              pointerId: event.pointerId,
              startX: event.clientX,
              startWidth: renderedWidth(),
              moved: false,
            };
          }}
          onPointerMove={(event) => {
            const drag = dragRef.current;
            if (drag?.pointerId !== event.pointerId) return;
            drag.moved ||= Math.abs(event.clientX - drag.startX) > DRAG_THRESHOLD;
            if (!drag.moved) return;

            if (drag.startWidth === 0) {
              // Dragging a closed panel away from the viewer has nothing to open.
              if (event.clientX >= drag.startX) return;

              // The panel opens at its minimum width, so the drag continues from there.
              drag.startWidth = MIN_SIDE_WIDTH;
              drag.startX = event.clientX;
            }
            resize(drag.startWidth + drag.startX - event.clientX);
          }}
          onPointerUp={(event) => {
            const drag = dragRef.current;
            if (drag?.pointerId !== event.pointerId) return;
            dragRef.current = null;
            if (!drag.moved) setOpen(!open);
          }}
          onPointerCancel={() => {
            dragRef.current = null;
          }}
          onLostPointerCapture={() => {
            dragRef.current = null;
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              setOpen(!open);
            } else if (open && event.key === "ArrowLeft") {
              event.preventDefault();
              resize(renderedWidth() + KEYBOARD_STEP);
            } else if (open && event.key === "ArrowRight") {
              event.preventDefault();
              resize(renderedWidth() - KEYBOARD_STEP);
            }
          }}
        >
          {open ? (
            <div className="h-8 w-1 rounded-full bg-border transition-colors group-hover:bg-primary/70 group-focus-visible:bg-primary/70" />
          ) : (
            <ChevronLeftIcon className="size-3 text-muted-foreground transition-colors group-hover:text-primary group-focus-visible:text-primary" />
          )}
        </div>
      ) : null}
      {side ? (
        // Hidden rather than unmounted to keep overlay menu pages and queued slider commits.
        <div
          ref={sideRef}
          className={cn("flex min-h-0 min-w-0 flex-col gap-3", resizable && !open && "lg:hidden")}
        >
          {side}
        </div>
      ) : null}
    </div>
  );
}
