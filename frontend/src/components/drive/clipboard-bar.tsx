import { Button } from "@renderer/components/ui/button";
import { useDriveClipboardActions } from "@renderer/hooks/use-drive-clipboard";
import { selectionStore } from "@renderer/store/drive";
import { CopyIcon, ScissorsIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useStore } from "zustand";

export function DriveClipboardBar({ destinationId }: { destinationId: string }) {
  const { t } = useTranslation();
  const { copyOrCuts, handlePaste } = useDriveClipboardActions(destinationId);
  const setCopyOrCuts = useStore(selectionStore, (s) => s.setCopyOrCuts);

  if (copyOrCuts.action === null || copyOrCuts.items.length === 0) return null;

  const Icon = copyOrCuts.action === "cut" ? ScissorsIcon : CopyIcon;
  const name = copyOrCuts.items[0].name;
  const message =
    copyOrCuts.items.length === 1
      ? t(`page.drive.clipboard.${copyOrCuts.action}_single`, { name })
      : t(`page.drive.clipboard.${copyOrCuts.action}_multiple`, {
          name,
          count: copyOrCuts.items.length - 1,
        });

  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-0 z-10">
      <div className="mx-auto w-full max-w-2xl px-4 pt-2 pb-4">
        <div
          role="status"
          className="pointer-events-auto flex items-center gap-3 rounded-md border bg-popover px-3 py-1.5 text-xs text-popover-foreground shadow-md"
        >
          <Icon className="size-3.5 shrink-0" />
          <span className="min-w-0 flex-1 truncate" title={message}>
            {message}
          </span>
          <Button variant="ghost" size="xs" onClick={() => setCopyOrCuts(null, [])}>
            {t("g.cancel")}
          </Button>
          <Button
            size="xs"
            disabled={destinationId === "share"}
            onClickPromise={async () => handlePaste()}
          >
            {t("page.drive.context_menu.paste")}
          </Button>
        </div>
      </div>
    </div>
  );
}

// Keeps the last rows reachable above the floating bar.
export function DriveClipboardSpacer() {
  const active = useStore(selectionStore, (s) => s.copyOrCuts.items.length > 0);
  return active ? <div className="h-14 shrink-0" /> : null;
}
