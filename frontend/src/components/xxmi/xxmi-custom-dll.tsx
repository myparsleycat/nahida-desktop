import { Dialog } from "@bindings/platform";
import { Button } from "@renderer/components/ui/button";
import { cn } from "@renderer/lib/utils";
import { useWindowFileDrop } from "@renderer/wails/file-drop";
import { toErrorMessage } from "@shared/utils";
import { FileUpIcon } from "lucide-react";
import { useState, type DragEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// Keep in sync with the custom DLL errors in internal/xxmi/custom_dll.go.
const customDllErrorCodes = ["XXMI_CUSTOM_DLL_INVALID", "XXMI_CUSTOM_DLL_MISSING"] as const;

// The custom DLL is offered beside the libraries providers, whose IDs it must not collide with.
export const CUSTOM_DLL_SOURCE = "__custom_dll__";

// Picks the custom d3d11.dll through the file dialog or a file dropped on the row. The row only reports the picked
// path; importing it and storing the selection is up to the caller, which differs between the shared and importer rows.
// A selection is dropped by choosing a provider again, so the row has no clear action of its own.
export function CustomDLLField({
  dropTargetId,
  selected,
  onPick,
}: {
  dropTargetId: string;
  selected?: { id: string; name?: string };
  onPick: (path: string) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [isDragOver, setIsDragOver] = useState(false);

  const pick = (path: string) =>
    onPick(path).catch((error: unknown) => {
      const message = toErrorMessage(error);
      const code = customDllErrorCodes.find((code) => message.includes(code));
      toast.error(code ? t(`page.setting.xxmi.builtin.customDllErrors.${code}`) : message);
    });

  useWindowFileDrop(({ paths, target }) => {
    if (target.id !== dropTargetId) return;
    setIsDragOver(false);
    if (paths.length !== 1 || !paths[0].toLowerCase().endsWith(".dll")) {
      toast.error(t("page.setting.xxmi.builtin.customDllOnlyDll"));
      return;
    }
    void pick(paths[0]);
  });

  const highlight = (event: DragEvent) => {
    if (event.dataTransfer.types.includes("Files")) setIsDragOver(true);
  };

  return (
    <div
      id={dropTargetId}
      data-file-drop-target
      onDragEnter={highlight}
      onDragOver={highlight}
      onDragLeave={(event) => {
        // Leaving for a child element also fires dragleave, so only a pointer outside the row clears the highlight.
        const rect = event.currentTarget.getBoundingClientRect();
        if (
          event.clientX <= rect.left ||
          event.clientX >= rect.right ||
          event.clientY <= rect.top ||
          event.clientY >= rect.bottom
        ) {
          setIsDragOver(false);
        }
      }}
      onDrop={() => setIsDragOver(false)}
      className={cn(
        "flex items-center justify-between gap-6 rounded-md transition-all duration-200",
        isDragOver && "bg-primary/5 ring-2 ring-primary/20",
      )}
    >
      <div className="min-w-0 flex-1 space-y-0.5">
        <span className="text-sm font-medium break-all">
          {selected?.name ?? t("page.setting.xxmi.builtin.customDll")}
        </span>
        <p className="text-xs break-all text-muted-foreground">
          {selected ? selected.id : t("page.setting.xxmi.builtin.customDllDropHint")}
        </p>
      </div>
      <div className="flex shrink-0 gap-2">
        <Button
          variant="outline"
          size="sm"
          onClickPromise={async () => {
            const picked = await Dialog.ShowOpenDialog({
              title: t("page.setting.xxmi.builtin.customDllSelect"),
              defaultPath: "",
              filters: [{ name: "DLL", extensions: ["dll"] }],
              properties: ["openFile"],
            }).catch((error: unknown) => {
              toast.error(toErrorMessage(error));
              return undefined;
            });
            if (!picked || picked.canceled || !picked.filePaths?.[0]) return;
            await pick(picked.filePaths[0]);
          }}
        >
          <FileUpIcon />
          {t("page.setting.xxmi.builtin.customDllSelect")}
        </Button>
      </div>
    </div>
  );
}
