import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { DownloadIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface CustomDownloadDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  groupName?: string;
  groupPath?: string;
}

export function CustomDownloadDialog({
  open,
  onOpenChange,
  groupName,
  groupPath,
}: CustomDownloadDialogProps) {
  const { t } = useTranslation();
  const [url, setUrl] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    if (!open) {
      setUrl("");
      setIsSubmitting(false);
    }
  }, [open]);

  const handleDownload = async () => {
    const trimmedUrl = url.trim();
    if (!groupPath || !trimmedUrl) {
      return;
    }

    try {
      new URL(trimmedUrl);
    } catch {
      toast.error(t("page.mod.content-header.download_dialog.invalid_url"));
      return;
    }

    try {
      setIsSubmitting(true);
      await window.api.invoke("mod:downloadFromUrl", trimmedUrl, groupPath);
      toast.success(t("page.mod.content-header.download_dialog.started"));
      onOpenChange(false);
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t("page.mod.content-header.download_dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("page.mod.content-header.download_dialog.description")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <p className="text-sm font-medium">
              {t("page.mod.content-header.download_dialog.url_label")}
            </p>
            <Input
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={t("page.mod.content-header.download_dialog.url_placeholder")}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  void handleDownload();
                }
              }}
            />
          </div>

          <div className="space-y-2">
            <p className="text-sm font-medium">
              {t("page.mod.content-header.download_dialog.target_label")}
            </p>
            <p className="rounded-md bg-muted px-3 py-2 text-sm text-muted-foreground">
              {groupName || t("page.mod.content-header.download_dialog.no_target")}
            </p>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
            {t("g.cancel")}
          </Button>
          <Button onClick={handleDownload} disabled={!groupPath || !url.trim() || isSubmitting}>
            <DownloadIcon className="mr-2 size-4" />
            {t("g.download")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
