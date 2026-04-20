import "@google/model-viewer";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { useEffect, useState } from "react";
import { modelViewerSourceToUrl, suppressModelViewerFocusOutline } from "./model-viewer-session";

export type ModelViewerDialogSource = {
  glbPath: string;
  name: string;
};

export function ModelViewerDialog({
  open,
  onOpenChange,
  source,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  source: ModelViewerDialogSource | null;
}) {
  const [objectUrl, setObjectUrl] = useState("");

  useEffect(() => {
    if (!source?.glbPath) {
      setObjectUrl("");
      return;
    }

    setObjectUrl(modelViewerSourceToUrl(source.glbPath));
  }, [source?.glbPath]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex h-[min(82vh,760px)] min-w-[min(92vw,1120px)] flex-col gap-3 p-3 focus:outline-none focus-visible:outline-none"
        onClick={(e) => e.stopPropagation()}
      >
        <DialogHeader className="pr-10">
          <DialogTitle>{source?.name || "Model Viewer"}</DialogTitle>
        </DialogHeader>

        <div className="relative min-h-0 flex-1 overflow-hidden rounded-md border bg-muted/30">
          {objectUrl ? (
            <model-viewer
              ref={suppressModelViewerFocusOutline}
              className="absolute inset-0 h-full w-full focus:outline-none focus-visible:outline-none"
              style={{ outline: "none" }}
              src={objectUrl}
              ar
              ar-modes="webxr scene-viewer quick-look"
              camera-controls
              tone-mapping="neutral"
              shadow-intensity="1"
              exposure="1"
            >
              <div className="progress-bar hide" slot="progress-bar">
                <div className="update-bar"></div>
              </div>
            </model-viewer>
          ) : (
            <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
              Model data is not available.
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
