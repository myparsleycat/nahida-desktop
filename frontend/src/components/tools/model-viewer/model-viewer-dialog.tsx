import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { ModelViewerDialogSource } from "./model-viewer-dialog-types";

import { ModelViewerWorkspace } from "./model-viewer-workspace";

export type { ModelViewerDialogSource } from "./model-viewer-dialog-types";

export function ModelViewerDialog({
  open,
  onOpenChange,
  source,
  existingPreviewPath,
  onPreviewSaved,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  source: ModelViewerDialogSource | null;
  existingPreviewPath?: string;
  onPreviewSaved?: () => void | Promise<void>;
}) {
  const { t } = useTranslation();
  const [doubleSidedEnabled, setDoubleSidedEnabled] = useState(true);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex h-full max-h-[92vh] min-w-[95vw] flex-col gap-3 p-3 focus:outline-none focus-visible:outline-none"
        onClick={(event) => event.stopPropagation()}
      >
        <DialogHeader className="pr-10">
          <DialogTitle className="truncate" title={source?.name}>
            {source?.name || t("page.tools.model_viewer.title")}
          </DialogTitle>
        </DialogHeader>
        <ModelViewerWorkspace
          open={open}
          doubleSidedEnabled={doubleSidedEnabled}
          onDoubleSidedChange={setDoubleSidedEnabled}
          source={source}
          existingPreviewPath={existingPreviewPath}
          onPreviewSaved={onPreviewSaved}
        />
      </DialogContent>
    </Dialog>
  );
}
