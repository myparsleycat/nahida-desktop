import TouchProfileTool from "@renderer/components/tools/touch-profile/touch-profile";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { useTranslation } from "react-i18next";

interface TouchProfileDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  modPath: string;
  modName: string;
  onApplied?: (result: { outputModRoot: string; sourceModRoot: string }) => void;
  onRolledBack?: (sourceModRoot: string) => void;
}

export function TouchProfileDialog({
  open,
  onOpenChange,
  modPath,
  modName,
  onApplied,
  onRolledBack,
}: TouchProfileDialogProps) {
  const { t } = useTranslation();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex h-full max-h-[92vh] min-w-[95vw] flex-col gap-3 p-3 focus:outline-none focus-visible:outline-none"
        onClick={(event) => event.stopPropagation()}
      >
        <DialogHeader className="pr-10">
          <DialogTitle className="truncate" title={modName}>
            {modName || `${t("page.tools.touch_profile.title")} (${t("g.beta")})`}
          </DialogTitle>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-hidden">
          {open ? (
            <TouchProfileTool
              fixedTargetPath={modPath}
              modName={modName}
              onApplied={onApplied}
              onRolledBack={onRolledBack}
            />
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}
