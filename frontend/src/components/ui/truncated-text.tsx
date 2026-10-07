import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { cn } from "@renderer/lib/utils";
import { useRef } from "react";

// The trigger renders a span, not a button: mod rows and cards ignore clicks that land on a button.
function TruncatedText({ text, className }: { text: string; className?: string }) {
  const ref = useRef<HTMLSpanElement>(null);

  return (
    <Tooltip
      disableHoverablePopup
      onOpenChange={(open, details) => {
        if (open && !isTruncated(ref.current)) {
          details.cancel();
        }
      }}
    >
      <TooltipTrigger render={<span ref={ref} />} className={cn("truncate", className)}>
        {text}
      </TooltipTrigger>
      <TooltipContent>
        <p className="text-wrap break-all">{text}</p>
      </TooltipContent>
    </Tooltip>
  );
}

// scrollWidth and clientWidth round to integers, so an overflow under one pixel still draws an
// ellipsis while both report the same value. Range rects keep the fraction and ignore clipping.
function isTruncated(element: HTMLElement | null) {
  if (!element) {
    return false;
  }

  const range = document.createRange();
  range.selectNodeContents(element);
  return range.getBoundingClientRect().width > element.getBoundingClientRect().width;
}

export { TruncatedText };
