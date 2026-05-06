import { Button } from "@renderer/components/ui/button";
import { XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

interface ModPreviewLightboxProps {
  preview: string;
  cacheKey?: string | number;
}

export function ModPreviewLightbox({ preview, cacheKey }: ModPreviewLightboxProps) {
  const [open, setOpen] = useState(false);
  const isVideo = preview.match(/\.(mp4|webm|ogg)$/i);
  const localSrc =
    cacheKey === undefined
      ? `local://${preview}`
      : `local://${preview}?v=${encodeURIComponent(String(cacheKey))}`;
  const origSrc = `${localSrc}${localSrc.includes("?") ? "&" : "?"}orig=true`;

  useEffect(() => {
    if (!open) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [open]);

  return (
    <>
      <div
        className="size-10 rounded-sm bg-secondary flex items-center justify-center overflow-hidden shrink-0 cursor-zoom-in"
        onClick={(e) => {
          e.stopPropagation();
          setOpen(true);
        }}
      >
        {isVideo ? (
          <video
            src={localSrc}
            className="w-full h-full object-cover"
            muted
            autoPlay
            loop
            controls={false}
          />
        ) : (
          <img src={localSrc} alt="preview" className="w-full h-full object-cover" />
        )}
      </div>

      {open &&
        createPortal(
          <div
            className="fixed inset-0 z-100 flex items-center justify-center bg-black/80 backdrop-blur-sm animate-in fade-in-0"
            onClick={(e) => {
              e.stopPropagation();
              setOpen(false);
            }}
            onContextMenu={(e) => e.stopPropagation()}
          >
            <Button
              variant="ghost"
              size="icon"
              className="absolute top-10 right-4 text-white hover:bg-white/20 hover:text-white"
              onClick={(e) => {
                e.stopPropagation();
                setOpen(false);
              }}
            >
              <XIcon className="size-6" />
            </Button>

            <div
              className="relative w-[80vw] h-[80vh] flex items-center justify-center cursor-zoom-out"
              onClick={(e) => {
                e.stopPropagation();
                setOpen(false);
              }}
            >
              {isVideo ? (
                <video
                  src={origSrc}
                  className="max-w-full max-h-full object-contain rounded-md shadow-2xl"
                  muted
                  autoPlay
                  loop
                  playsInline
                  controls={false}
                />
              ) : (
                <img
                  src={origSrc}
                  alt="preview full"
                  className="max-w-full max-h-full object-contain rounded-md shadow-2xl"
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
