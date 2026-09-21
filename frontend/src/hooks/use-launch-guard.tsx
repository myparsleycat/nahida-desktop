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
import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// Keep in sync with errGimiDCREnabled and errSmoothMotionEnabled in internal/xxmi/launch_guard.go.
const LAUNCH_BLOCKER_DCR = "GIMI_DCR_ENABLED";
const LAUNCH_BLOCKER_SMOOTH_MOTION = "NVIDIA_SMOOTH_MOTION_ENABLED";

type LaunchDialog = "gimi-dcr" | "smooth-motion" | "launch-blockers";

export type LaunchGuardResult = { status: "started" } | { status: "blocked"; kind: LaunchDialog };

export function launchDialog(message: string): LaunchDialog | null {
  const dcr = message.includes(LAUNCH_BLOCKER_DCR);
  const smoothMotion = message.includes(LAUNCH_BLOCKER_SMOOTH_MOTION);
  if (dcr && smoothMotion) {
    return "launch-blockers";
  }
  if (dcr) {
    return "gimi-dcr";
  }
  if (smoothMotion) {
    return "smooth-motion";
  }
  return null;
}

export function useLaunchGuard() {
  const { t } = useTranslation();
  const [pendingImporter, setPendingImporter] = useState<string | null>(null);
  const [dialog, setDialog] = useState<LaunchDialog>("gimi-dcr");
  const [isConfirming, setIsConfirming] = useState(false);
  const confirmGeneration = useRef(0);

  const startImporter = useCallback(async (importer: string): Promise<LaunchGuardResult> => {
    try {
      await XXMI.StartGame(importer);
      return { status: "started" };
    } catch (error) {
      const kind = launchDialog(toErrorMessage(error));
      if (!kind) {
        throw error;
      }
      setDialog(kind);
      setPendingImporter(importer);
      return { status: "blocked", kind };
    }
  }, []);

  // Bumping the generation cancels any in-flight confirmation, so dismissing the dialog
  // while ClearLaunchBlockers is still running does not launch the game afterwards.
  const closeDialog = useCallback(() => {
    confirmGeneration.current += 1;
    setIsConfirming(false);
    setPendingImporter(null);
  }, []);

  const handleConfirm = useCallback(async () => {
    if (!pendingImporter) {
      return;
    }
    const importer = pendingImporter;
    const generation = (confirmGeneration.current += 1);
    setIsConfirming(true);

    try {
      await XXMI.ClearLaunchBlockers(importer);
    } catch (error) {
      if (confirmGeneration.current !== generation) {
        return;
      }
      const message = toErrorMessage(error);
      const kind = launchDialog(message);
      if (kind) {
        setDialog(kind);
        return;
      }
      toast.error(message);
      return;
    } finally {
      if (confirmGeneration.current === generation) {
        setIsConfirming(false);
      }
    }

    if (confirmGeneration.current !== generation) {
      return;
    }
    setPendingImporter(null);

    // The blockers were just cleared for this importer, so a rejection here means the fix did
    // not take effect. Surface it instead of reopening the dialog and looping forever.
    try {
      await XXMI.StartGame(importer);
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  }, [pendingImporter]);

  const alert = useMemo(
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
            <AlertDialogTitle>{t(`page.mod.dialog.${dialog}.title`)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(`page.mod.dialog.${dialog}.description`)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isConfirming}>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction disabled={isConfirming} onClickPromise={handleConfirm}>
              {t(`page.mod.dialog.${dialog}.confirm`)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    ),
    [closeDialog, dialog, handleConfirm, isConfirming, pendingImporter, t],
  );

  return { startImporter, launchGuardDialog: alert };
}
