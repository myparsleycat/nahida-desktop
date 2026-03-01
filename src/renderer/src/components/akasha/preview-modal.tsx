import { VisuallyHidden } from "@radix-ui/react-visually-hidden";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@renderer/components/ui/dialog";
import { useDialogStore } from "@renderer/store/drive";
import { ContentPreview, LayoutType } from "@renderer/types";
import { useState } from "react";

interface Props {
  className?: string;
  preview?: ContentPreview;
  alt: string;
  type: LayoutType;
}

export function PreviewModal(props: Props) {
  if (!props.preview) return null;

  const [open, setOpen] = useState(false);

  const dialog = useDialogStore();

  function imgErrorHandle(e: React.SyntheticEvent) {
    if (e.currentTarget) {
      // @ts-ignore
      e.currentTarget.src = "https://nahida.live/puhaha.jpg";
    }
  }

  const hasVideo = !!props.preview.video?.default;

  const onMouseEvent = (e: React.MouseEvent<HTMLDivElement, MouseEvent>) => {
    e.preventDefault();
    e.stopPropagation();
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        dialog.setOpen("previewDialog", v);
      }}
    >
      <DialogTrigger className={props.className} onClick={(e) => e.stopPropagation()}>
        {hasVideo ? (
          <video
            src={props.preview.video?.default}
            className="object-cover rounded-md aspect-square"
            draggable="false"
            muted
            autoPlay
            loop
            controls={false}
          />
        ) : (
          <img
            src={
              props.type === "list"
                ? props.preview!.img!.thumbnail || props.preview!.img!.default
                : props.preview!.img!.cover || props.preview!.img!.default
            }
            alt={props.alt}
            className="object-cover rounded-md aspect-square"
            draggable="false"
            loading="lazy"
            onError={imgErrorHandle}
          />
        )}
      </DialogTrigger>
      <DialogContent
        aria-describedby={undefined}
        showCloseButton={false}
        className="size-fit p-0 overflow-hidden sm:max-w-none"
        onClick={onMouseEvent}
        onContextMenu={onMouseEvent}
        overlayOnContextMenu={onMouseEvent}
      >
        <VisuallyHidden>
          <DialogHeader>
            <DialogTitle></DialogTitle>
          </DialogHeader>
        </VisuallyHidden>

        {hasVideo ? (
          <video
            src={props.preview.video?.default}
            draggable="false"
            muted
            autoPlay
            loop
            controls={false}
            className="max-w-[85vw] max-h-[85vh] cursor-pointer"
            onClick={() => setOpen(false)}
          />
        ) : (
          <button onClick={() => setOpen(false)}>
            <img
              src={props.preview.img?.default}
              alt={props.alt}
              draggable="false"
              className="max-w-[85vw] max-h-[85vh]"
            />
          </button>
        )}
      </DialogContent>
    </Dialog>
  );
}
