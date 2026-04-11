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
import { DownloadIcon, LoaderIcon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import isURL from "validator/lib/isURL";

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
  const trimmedUrl = url.trim();
  const isValidUrl = isURL(trimmedUrl);

  useEffect(() => {
    if (!open) {
      setUrl("");
      setIsSubmitting(false);
    }
  }, [open]);

  const handleDownload = async () => {
    if (!groupPath || !trimmedUrl) {
      return;
    }

    if (!isValidUrl) {
      toast.warning(t("page.mod.content-header.download_dialog.invalid_url"));
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
            <Input
              readOnly
              hideFocusRing
              value={groupName || t("page.mod.content-header.download_dialog.no_target")}
            />
          </div>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            size="icon"
            onClick={() => onOpenChange(false)}
            disabled={isSubmitting}
          >
            <XIcon />
          </Button>
          <Button
            onClick={handleDownload}
            disabled={!groupPath || !trimmedUrl || !isValidUrl || isSubmitting}
            size="icon"
          >
            {isSubmitting ? (
              <LoaderIcon className="size-4 animate-spin" />
            ) : (
              <DownloadIcon className="size-4" />
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
