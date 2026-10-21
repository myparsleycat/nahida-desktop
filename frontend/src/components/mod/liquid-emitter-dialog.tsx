import { LiquidEmitterTool } from "@renderer/components/tools/liquid-emitter/liquid-emitter";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { useState } from "react";
import { useTranslation } from "react-i18next";

interface LiquidEmitterDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  modPath: string;
  modName: string;
  /** Called after the tool created a new mod or restored the original. */
  onModsChanged?: () => void;
}

export function LiquidEmitterDialog({
  open,
  onOpenChange,
  modPath,
  modName,
  onModsChanged,
}: LiquidEmitterDialogProps) {
  const { t } = useTranslation();
  // The folder the tool moved to, kept only while the dialog shows the mod it was opened for.
  const [moved, setMoved] = useState<{ from: string; to: string } | null>(null);
  const [unsaved, setUnsaved] = useState(false);
  const [confirmClose, setConfirmClose] = useState(false);
  const currentPath = moved?.from === modPath ? moved.to : modPath;
  const currentName =
    currentPath === modPath ? modName : (currentPath.split(/[\\/]/).pop() ?? currentPath);
  const close = () => {
    setMoved(null);
    setConfirmClose(false);
    onOpenChange(false);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) onOpenChange(true);
        else if (unsaved) setConfirmClose(true);
        else close();
      }}
    >
      <DialogContent
        className="flex h-full max-h-[92vh] min-w-[95vw] flex-col gap-3 p-3 focus:outline-none focus-visible:outline-none"
        onClick={(event) => event.stopPropagation()}
      >
        <DialogHeader className="pr-10">
          <DialogTitle className="truncate" title={currentName}>
            {currentName || `${t("page.tools.liquid_emitter.title")} (${t("g.beta")})`}
          </DialogTitle>
        </DialogHeader>

        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          {open && currentPath ? (
            <LiquidEmitterTool
              key={currentPath}
              modPath={currentPath}
              onModPathChange={(next) => {
                setMoved({ from: modPath, to: next });
                onModsChanged?.();
              }}
              onUnsavedChange={setUnsaved}
            />
          ) : null}
        </div>

        <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
          <AlertDialogContent onClick={(event) => event.stopPropagation()}>
            <AlertDialogHeader>
              <AlertDialogTitle>{t("page.tools.liquid_emitter.unsaved.title")}</AlertDialogTitle>
              <AlertDialogDescription>
                {t("page.tools.liquid_emitter.unsaved.description")}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={close}>
                {t("page.tools.liquid_emitter.unsaved.confirm")}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </DialogContent>
    </Dialog>
  );
}
