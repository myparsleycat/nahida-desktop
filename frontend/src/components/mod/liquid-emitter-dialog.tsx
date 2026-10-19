import { LiquidEmitterTool } from "@renderer/components/tools/liquid-emitter/liquid-emitter";
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
  const currentPath = moved?.from === modPath ? moved.to : modPath;
  const currentName =
    currentPath === modPath ? modName : (currentPath.split(/[\\/]/).pop() ?? currentPath);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setMoved(null);
        onOpenChange(next);
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
            />
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}
