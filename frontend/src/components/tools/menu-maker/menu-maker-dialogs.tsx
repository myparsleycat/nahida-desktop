import { Button } from "@renderer/components/ui/button";
import {
  Dialog as ModalDialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Logger } from "@renderer/lib/logger";
import { cn } from "@renderer/lib/utils";
import { deleteDraftBlobs, loadDraftMetadata, saveDraftMetadata } from "@shared/menu-maker/drafts";
import { XIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

export function DraftDialog({
  onRestore,
  onClose,
  t,
}: {
  onRestore: (draft: ReturnType<typeof loadDraftMetadata>[number]) => Promise<void>;
  onClose: () => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  const [drafts, setDrafts] = useState(loadDraftMetadata);
  return (
    <Modal title={t("page.tools.menu_maker.drafts")} onClose={onClose}>
      <div className="max-h-[60vh] space-y-2 overflow-auto">
        {drafts.map((draft) => (
          <div key={draft.id} className="flex items-center gap-2 rounded border border-border p-3">
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm">{draft.sourceName}</p>
              <p className="text-xs text-muted-foreground">
                {new Date(draft.updatedAt).toLocaleString()}
              </p>
            </div>
            <Button size="sm" onClick={() => void onRestore(draft)}>
              {t("page.tools.menu_maker.restore")}
            </Button>
            <Button
              size="icon"
              variant="ghost"
              onClick={() => {
                void deleteDraftBlobs(draft.id).catch((error) =>
                  Logger.error({ error, draftId: draft.id }, "MenuMakerPage:deleteDraftBlobs"),
                );
                setDrafts(saveDraftMetadata(drafts.filter((item) => item.id !== draft.id)));
              }}
            >
              <XIcon />
            </Button>
          </div>
        ))}
      </div>
      {drafts.length > 0 && (
        <Button
          className="mt-3"
          variant="destructive"
          onClick={() => {
            void Promise.all(drafts.map((draft) => deleteDraftBlobs(draft.id))).catch((error) =>
              Logger.error({ error }, "MenuMakerPage:clearDraftBlobs"),
            );
            setDrafts(saveDraftMetadata([]));
          }}
        >
          {t("page.tools.menu_maker.delete_all")}
        </Button>
      )}
    </Modal>
  );
}

export function CompareDialog({
  original,
  generated,
  onClose,
  t,
}: {
  original: string;
  generated: string;
  onClose: () => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  return (
    <Modal title={t("page.tools.menu_maker.compare")} onClose={onClose} wide>
      <div className="grid max-h-[70vh] grid-cols-1 gap-3 md:grid-cols-2">
        <div>
          <h3 className="mb-1 text-xs font-medium">{t("page.tools.menu_maker.original_logic")}</h3>
          <pre className="h-[60vh] overflow-auto rounded bg-muted p-3 text-[11px] select-text">
            {original}
          </pre>
        </div>
        <div>
          <h3 className="mb-1 text-xs font-medium">{t("page.tools.menu_maker.generated_logic")}</h3>
          <pre className="h-[60vh] overflow-auto rounded bg-muted p-3 text-[11px] select-text">
            {generated}
          </pre>
        </div>
      </div>
    </Modal>
  );
}

export function Modal({
  title,
  onClose,
  wide,
  children,
}: {
  title: string;
  onClose: () => void;
  wide?: boolean;
  children: React.ReactNode;
}) {
  return (
    <ModalDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className={cn("sm:max-w-2xl", wide && "max-w-[calc(100%-2rem)] sm:max-w-6xl")}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        {children}
      </DialogContent>
    </ModalDialog>
  );
}
