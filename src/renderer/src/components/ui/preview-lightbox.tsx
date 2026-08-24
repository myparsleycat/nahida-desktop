import { Button } from "@renderer/components/ui/button";
import { XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

interface PreviewLightboxProps {
  thumbnailSrc: string;
  fullSrc: string;
  isVideo: boolean;
  alt?: string;
  className?: string;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

export function PreviewLightbox(props: PreviewLightboxProps) {
  const [internalOpen, setInternalOpen] = useState(false);
  const isControlled = props.open !== undefined;

  const open = isControlled ? props.open : internalOpen;

  const change = (next: boolean) => {
    if (!isControlled) {
      setInternalOpen(next);
    }
    props.onOpenChange?.(next);
  };

  useEffect(() => {
    if (!open) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") change(false);
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [open]);

  return (
    <>
      {!isControlled && (
        <div
          className={props.className}
          onClick={(e) => {
            e.stopPropagation();
            change(true);
          }}
        >
          {props.isVideo ? (
            <video
              src={props.thumbnailSrc}
              className="h-full w-full object-cover"
              muted
              autoPlay
              loop
              playsInline
              controls={false}
            />
          ) : (
            <img
              src={props.thumbnailSrc}
              alt={props.alt}
              className="h-full w-full object-cover"
              draggable={false}
            />
          )}
        </div>
      )}

      {open &&
        createPortal(
          <div
            className="fixed inset-0 z-100 flex animate-in items-center justify-center bg-black/80 backdrop-blur-sm fade-in-0"
            onClick={(e) => {
              e.stopPropagation();
              change(false);
            }}
            onContextMenu={(e) => e.stopPropagation()}
          >
            <Button
              variant="ghost"
              size="icon"
              className="absolute top-10 right-4 text-white hover:bg-white/20 hover:text-white"
              onClick={(e) => {
                e.stopPropagation();
                change(false);
              }}
            >
              <XIcon className="size-6" />
            </Button>

            <div
              className="relative flex h-[80vh] w-[80vw] cursor-zoom-out items-center justify-center"
              onClick={(e) => {
                e.stopPropagation();
                change(false);
              }}
            >
              {props.isVideo ? (
                <video
                  src={props.fullSrc}
                  className="max-h-full max-w-full rounded-md object-contain shadow-2xl"
                  muted
                  autoPlay
                  loop
                  playsInline
                  controls={false}
                />
              ) : (
                <img
                  src={props.fullSrc}
                  alt={props.alt}
                  className="max-h-full max-w-full rounded-md object-contain shadow-2xl"
                  draggable={false}
                />
              )}
            </div>
          </div>,
          document.body,
        )}
    </>
  );
}
