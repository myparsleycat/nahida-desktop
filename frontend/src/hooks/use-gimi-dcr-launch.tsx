import { XXMI } from "@bindings/xxmi";
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
import { toErrorMessage } from "@shared/utils";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const GIMI_DCR_ENABLED = "GIMI_DCR_ENABLED";

export function useGimiDCRLaunch() {
  const { t } = useTranslation();
  const [pendingImporter, setPendingImporter] = useState<string | null>(null);
  const [isConfirming, setIsConfirming] = useState(false);

  const startImporter = useCallback(async (importer: string) => {
    try {
      await XXMI.StartGame(importer);
    } catch (error) {
      if (toErrorMessage(error).includes(GIMI_DCR_ENABLED)) {
        setPendingImporter(importer);
        return;
      }
      throw error;
    }
  }, []);

  const closeDialog = useCallback(() => {
    if (isConfirming) {
      return;
    }
    setPendingImporter(null);
  }, [isConfirming]);

  const handleConfirm = useCallback(async () => {
    if (!pendingImporter) {
      return;
    }
    const importer = pendingImporter;
    setIsConfirming(true);
    try {
      await XXMI.DisableGenshinDynamicCharacterResolution();
      await XXMI.StartGame(importer);
      setPendingImporter(null);
    } catch (error) {
      toast.error(toErrorMessage(error));
    } finally {
      setIsConfirming(false);
    }
  }, [pendingImporter]);

  const dialog = useMemo(
    () => (
      <AlertDialog
        open={pendingImporter !== null}
        onOpenChange={(open) => {
          if (!open) {
            closeDialog();
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.gimi-dcr.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.gimi-dcr.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isConfirming}>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction disabled={isConfirming} onClickPromise={handleConfirm}>
              {t("page.mod.dialog.gimi-dcr.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    ),
    [closeDialog, handleConfirm, isConfirming, pendingImporter, t],
  );

  return { startImporter, gimiDCRDialog: dialog };
}
