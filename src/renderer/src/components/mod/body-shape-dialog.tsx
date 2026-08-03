import BodyShapeTool from "@renderer/components/tools/body-shape/body-shape";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { useTranslation } from "react-i18next";

interface BodyShapeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  modPath: string;
  modName: string;
  onExported?: (result: { modRoot?: string; sourceModPath?: string }) => void;
}

export function BodyShapeDialog({
  open,
  onOpenChange,
  modPath,
  modName,
  onExported,
}: BodyShapeDialogProps) {
  const { t } = useTranslation();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex h-full max-h-[92vh] min-w-[95vw] flex-col gap-3 p-3 focus:outline-none focus-visible:outline-none"
        onClick={(event) => event.stopPropagation()}
      >
        <DialogHeader className="pr-10">
          <DialogTitle className="truncate" title={modName}>
            {modName || t("page.tools.body_shape.title")}
          </DialogTitle>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-hidden">
          {open ? (
            <BodyShapeTool fixedTargetPath={modPath} modName={modName} onExported={onExported} />
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}
