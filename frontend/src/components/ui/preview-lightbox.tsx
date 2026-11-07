import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { Button } from "@renderer/components/ui/button";
import { cn } from "@renderer/lib/utils";
import { ChevronLeftIcon, ChevronRightIcon, XIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

interface PreviewLightboxGallery {
  index: number;
  count: number;
  onIndexChange: (index: number) => void;
}

interface PreviewLightboxBaseProps {
  fullSrc: string;
  isVideo: boolean;
  alt?: string;
  onOpenChange?: (open: boolean) => void;
  gallery?: PreviewLightboxGallery;
}

type PreviewLightboxProps = PreviewLightboxBaseProps &
  (
    | { open?: undefined; thumbnailSrc: string; className?: string }
    | { open: boolean; thumbnailSrc?: never; className?: never }
  );

export function PreviewLightbox(props: PreviewLightboxProps) {
  const { t } = useTranslation();
  const [internalOpen, setInternalOpen] = useState(false);
  const isControlled = props.open !== undefined;

  const open = isControlled ? props.open : internalOpen;
  const gallery = props.gallery && props.gallery.count > 1 ? props.gallery : undefined;

  const change = (next: boolean) => {
    if (!isControlled) {
      setInternalOpen(next);
    }
    props.onOpenChange?.(next);
  };

  const step = (direction: -1 | 1) => {
    if (!gallery) return;
    gallery.onIndexChange((gallery.index + direction + gallery.count) % gallery.count);
  };

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

      <DialogPrimitive.Root open={open} onOpenChange={(next) => change(next)}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Popup
            aria-label={props.alt}
            finalFocus={false}
            className="fixed inset-0 z-100 flex animate-in items-center justify-center bg-black/80 backdrop-blur-sm fade-in-0 outline-none"
            onClick={(e) => {
              e.stopPropagation();
              change(false);
            }}
            onContextMenu={(e) => e.stopPropagation()}
            onKeyDown={(e) => {
              if (gallery && (e.key === "ArrowLeft" || e.key === "ArrowRight")) {
                e.preventDefault();
                step(e.key === "ArrowLeft" ? -1 : 1);
              }
            }}
          >
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("g.close")}
              className="absolute top-10 right-4 text-white hover:bg-white/20 hover:text-white"
              onClick={(e) => {
                e.stopPropagation();
                change(false);
              }}
            >
              <XIcon className="size-6" />
            </Button>

            {gallery &&
              ([-1, 1] as const).map((direction) => (
                <Button
                  key={direction}
                  variant="ghost"
                  size="icon"
                  aria-label={direction === -1 ? t("g.previous") : t("g.next")}
                  className={cn(
                    "absolute text-white hover:bg-white/20 hover:text-white",
                    direction === -1 ? "left-4" : "right-4",
                  )}
                  onClick={(e) => {
                    e.stopPropagation();
                    step(direction);
                  }}
                >
                  {direction === -1 ? (
                    <ChevronLeftIcon className="size-6" />
                  ) : (
                    <ChevronRightIcon className="size-6" />
                  )}
                </Button>
              ))}

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

            {gallery && (
              <div className="absolute bottom-6 rounded-full bg-black/50 px-3 py-1 text-xs text-white">
                {gallery.index + 1} / {gallery.count}
              </div>
            )}
          </DialogPrimitive.Popup>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </>
  );
}
