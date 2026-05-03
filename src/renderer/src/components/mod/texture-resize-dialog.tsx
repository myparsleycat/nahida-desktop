import { TextureResizerWorkspace } from "@renderer/components/tools/texture-resizer-workspace";
import { Dialog, DialogContent } from "@renderer/components/ui/dialog";

interface TextureResizeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  modPath: string;
  modName: string;
}

export function TextureResizeDialog({
  open,
  onOpenChange,
  modPath,
  modName,
}: TextureResizeDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex h-[min(85vh,900px)] w-[min(80rem,calc(100%-32rem))] max-w-none flex-col overflow-hidden sm:max-w-none"
        onClick={(event) => event.stopPropagation()}
      >
        <TextureResizerWorkspace mode="mod" modName={modName} fixedTargetPath={modPath} />
      </DialogContent>
    </Dialog>
  );
}
