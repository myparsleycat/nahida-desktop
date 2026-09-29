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
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { toErrorMessage } from "@shared/utils";
import { useNavigate } from "@tanstack/react-router";
import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// Keep in sync with errGimiDCREnabled and errSmoothMotionEnabled in internal/xxmi/launch_guard.go.
const LAUNCH_BLOCKER_DCR = "GIMI_DCR_ENABLED";
const LAUNCH_BLOCKER_SMOOTH_MOTION = "NVIDIA_SMOOTH_MOTION_ENABLED";
const LAUNCH_BLOCKER_WWMI_WOUNDED = "WWMI_WOUNDED_FX_DECISION_REQUIRED";
const LAUNCH_BLOCKER_GAME_FOLDER = "XXMI_GAME_FOLDER_NOT_CONFIGURED";
const LAUNCH_BLOCKER_RUNTIME = "XXMI_RUNTIME_CORRUPTED";
const LAUNCH_BLOCKER_OLD_LIBS = "XXMI_LIBS_TOO_OLD";
const launchErrorCodes = [
  "XXMI_BUSY",
  "XXMI_GAME_RUNNING",
  "XXMI_RUNTIME_LOCKED",
  "XXMI_ELEVATION_DENIED",
  "XXMI_LOADER_TOO_OLD",
  "XXMI_INJECT_FAILED",
  "XXMI_GAME_START_TIMEOUT",
  "XXMI_LEGACY_LOADER_RUNNING",
  "XXMI_LEGACY_LOADER_EXITED",
  "XXMI_LEGACY_LOADER_NOT_READY",
  "GIMI_FPS_UNLOCKER_RUNNING",
] as const;

type LaunchDialog =
  | "gimi-dcr"
  | "smooth-motion"
  | "launch-blockers"
  | "wwmi-wounded"
  | "game-folder"
  | "runtime-repair"
  | "old-libs";

export type LaunchGuardResult =
  | { status: "started" }
  | { status: "blocked"; kind: LaunchDialog | "importer-setup" | "launch-error" };

export function launchErrorCode(message: string): (typeof launchErrorCodes)[number] | null {
  return launchErrorCodes.find((code) => message.includes(code)) ?? null;
}

export function launchDialog(message: string): LaunchDialog | null {
  if (message.includes(LAUNCH_BLOCKER_GAME_FOLDER)) {
    return "game-folder";
  }
  if (message.includes(LAUNCH_BLOCKER_RUNTIME)) {
    return "runtime-repair";
  }
  if (message.includes(LAUNCH_BLOCKER_OLD_LIBS)) {
    return "old-libs";
  }
  if (message.includes(LAUNCH_BLOCKER_WWMI_WOUNDED)) {
    return "wwmi-wounded";
  }
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
  const navigate = useNavigate();
  const [pendingImporter, setPendingImporter] = useState<string | null>(null);
  const [dialog, setDialog] = useState<LaunchDialog>("gimi-dcr");
  const [isConfirming, setIsConfirming] = useState(false);
  const [runtimeError, setRuntimeError] = useState<string | null>(null);
  const [gameFolder, setGameFolder] = useState("");
  const [detectedFolders, setDetectedFolders] = useState<
    Awaited<ReturnType<typeof XXMI.DetectGameFolders>> | undefined
  >();
  const confirmGeneration = useRef(0);

  const startImporter = useCallback(
    async (importer: string): Promise<LaunchGuardResult> => {
      try {
        await XXMI.StartGame(importer);
        return { status: "started" };
      } catch (error) {
        const message = toErrorMessage(error);
        if (
          message.includes("XXMI_NOT_CONFIGURED") ||
          message.includes("XXMI_IMPORTER_NOT_INSTALLED")
        ) {
          toast.info(t("page.setting.xxmi.builtin.importerSetupRequired", { importer }));
          await navigate({ to: "/setting/xxmi/$importer", params: { importer } });
          return { status: "blocked", kind: "importer-setup" };
        }
        const kind = launchDialog(message);
        if (!kind) {
          const code = launchErrorCode(message);
          if (code) {
            toast.error(t(`page.setting.xxmi.builtin.launchErrors.${code}`), {
              description: message,
            });
            return { status: "blocked", kind: "launch-error" };
          }
          throw error;
        }
        setDialog(kind);
        setRuntimeError(kind === "runtime-repair" ? message : null);
        setPendingImporter(importer);
        return { status: "blocked", kind };
      }
    },
    [navigate, t],
  );

  // Bumping the generation cancels any in-flight confirmation, so dismissing the dialog
  // while ClearLaunchBlockers is still running does not launch the game afterwards.
  const closeDialog = useCallback(() => {
    confirmGeneration.current += 1;
    setIsConfirming(false);
    setPendingImporter(null);
    setRuntimeError(null);
    setGameFolder("");
    setDetectedFolders(undefined);
  }, []);

  const handleConfirm = useCallback(async () => {
    if (!pendingImporter) {
      return;
    }
    const importer = pendingImporter;
    const generation = (confirmGeneration.current += 1);
    setIsConfirming(true);

    try {
      if (dialog === "runtime-repair") {
        const warnings = await XXMI.RepairRuntime(importer);
        warnings?.forEach((warning) => toast.warning(warning));
      } else if (dialog === "game-folder") {
        await XXMI.ValidateGameFolder(importer, gameFolder);
        const config = await XXMI.GetImporterConfig(importer);
        await XXMI.SaveImporterConfig(importer, { ...config, gameFolder });
      } else if (dialog === "wwmi-wounded") {
        const config = await XXMI.GetImporterConfig(importer);
        if (!config.wwmi) throw new Error("WWMI settings are unavailable");
        await XXMI.SaveImporterConfig(importer, {
          ...config,
          woundedFXDecided: true,
          wwmi: { ...config.wwmi, disableWoundedFX: true },
        });
      } else if (dialog !== "old-libs") {
        await XXMI.ClearLaunchBlockers(importer);
      }
    } catch (error) {
      if (confirmGeneration.current !== generation) {
        return;
      }
      const message = toErrorMessage(error);
      const kind = launchDialog(message);
      if (kind) {
        setDialog(kind);
        setRuntimeError(kind === "runtime-repair" ? message : null);
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
      if (dialog === "old-libs") {
        await XXMI.StartGameWithCompatibility(importer, true);
      } else {
        await XXMI.StartGame(importer);
      }
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  }, [dialog, gameFolder, pendingImporter]);

  const handleKeepWounded = useCallback(async () => {
    if (!pendingImporter) return;
    const importer = pendingImporter;
    const generation = (confirmGeneration.current += 1);
    setIsConfirming(true);
    try {
      const config = await XXMI.GetImporterConfig(importer);
      if (!config.wwmi) throw new Error("WWMI settings are unavailable");
      await XXMI.SaveImporterConfig(importer, {
        ...config,
        woundedFXDecided: true,
        wwmi: { ...config.wwmi, disableWoundedFX: false },
      });
      if (confirmGeneration.current !== generation) return;
      setPendingImporter(null);
      await XXMI.StartGame(importer);
    } catch (error) {
      if (confirmGeneration.current === generation) toast.error(toErrorMessage(error));
    } finally {
      if (confirmGeneration.current === generation) setIsConfirming(false);
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
            <AlertDialogTitle>
              {t(
                dialog === "game-folder"
                  ? "page.setting.xxmi.builtin.gameFolder"
                  : dialog === "old-libs"
                    ? "page.setting.xxmi.builtin.oldLibsTitle"
                    : dialog === "runtime-repair"
                      ? "page.setting.xxmi.builtin.repairRuntime"
                      : `page.mod.dialog.${dialog}.title`,
              )}
            </AlertDialogTitle>
            {dialog !== "game-folder" && (
              <AlertDialogDescription>
                {t(
                  dialog === "old-libs"
                    ? "page.setting.xxmi.builtin.oldLibsPrompt"
                    : dialog === "runtime-repair"
                      ? "page.setting.xxmi.builtin.runtimeRepairPrompt"
                      : `page.mod.dialog.${dialog}.description`,
                )}
              </AlertDialogDescription>
            )}
            {dialog === "runtime-repair" && runtimeError && (
              <p className="max-h-24 overflow-auto text-xs break-all text-muted-foreground">
                {runtimeError}
              </p>
            )}
          </AlertDialogHeader>
          {dialog === "game-folder" && (
            <div className="space-y-2">
              <div className="flex gap-2">
                <Input
                  aria-label={t("page.setting.xxmi.builtin.gameFolder")}
                  value={gameFolder}
                  onChange={(event) => setGameFolder(event.target.value)}
                />
                <Button
                  variant="outline"
                  disabled={isConfirming}
                  onClickPromise={async () => {
                    if (!pendingImporter) return;
                    try {
                      setDetectedFolders(await XXMI.DetectGameFolders(pendingImporter));
                    } catch (error) {
                      toast.error(toErrorMessage(error));
                    }
                  }}
                >
                  {t("page.setting.xxmi.builtin.detectGame")}
                </Button>
              </div>
              {detectedFolders && (
                <div className="max-h-48 space-y-1 overflow-y-auto">
                  {detectedFolders.length ? (
                    detectedFolders.map((candidate) => (
                      <Button
                        key={candidate.exePath}
                        variant="outline"
                        className="h-auto w-full justify-start text-left break-all whitespace-normal"
                        onClick={() => setGameFolder(candidate.path)}
                      >
                        {candidate.path}
                      </Button>
                    ))
                  ) : (
                    <p>{t("page.setting.xxmi.builtin.noGameFolders")}</p>
                  )}
                </div>
              )}
            </div>
          )}
          <AlertDialogFooter>
            {dialog === "wwmi-wounded" ? (
              <AlertDialogAction
                variant="outline"
                disabled={isConfirming}
                onClickPromise={handleKeepWounded}
              >
                {t("page.mod.dialog.wwmi-wounded.keep")}
              </AlertDialogAction>
            ) : (
              <AlertDialogCancel disabled={isConfirming}>{t("g.cancel")}</AlertDialogCancel>
            )}
            <AlertDialogAction
              disabled={isConfirming || (dialog === "game-folder" && !gameFolder.trim())}
              onClickPromise={handleConfirm}
            >
              {t(
                dialog === "game-folder"
                  ? "g.save"
                  : dialog === "old-libs"
                    ? "page.setting.xxmi.builtin.launchAnyway"
                    : dialog === "runtime-repair"
                      ? "page.setting.xxmi.builtin.repairRuntime"
                      : `page.mod.dialog.${dialog}.confirm`,
              )}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    ),
    [
      closeDialog,
      detectedFolders,
      dialog,
      gameFolder,
      handleConfirm,
      handleKeepWounded,
      isConfirming,
      pendingImporter,
      runtimeError,
      t,
    ],
  );

  return { startImporter, launchGuardDialog: alert };
}
