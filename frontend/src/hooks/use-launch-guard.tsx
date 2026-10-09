import type { PresetEffects } from "@bindings/reshade";
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
import { ButtonGroup } from "@renderer/components/ui/button-group";
import { Input } from "@renderer/components/ui/input";
import { ReShadePresetEffectsDialog } from "@renderer/components/xxmi/reshade-preset-effects-dialog";
import { XXMIUpdateDialog, type UpdateStatus } from "@renderer/components/xxmi/xxmi-update-dialog";
import { toErrorMessage } from "@shared/utils";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// Keep in sync with errGimiDCREnabled and errSmoothMotionEnabled in internal/xxmi/launch_guard.go.
const LAUNCH_BLOCKER_DCR = "GIMI_DCR_ENABLED";
const LAUNCH_BLOCKER_SMOOTH_MOTION = "NVIDIA_SMOOTH_MOTION_ENABLED";
const LAUNCH_BLOCKER_WWMI_WOUNDED = "WWMI_WOUNDED_FX_DECISION_REQUIRED";
// Keep in sync with errWWMIResourceTierUndecided in internal/xxmi/launch_builtin.go.
const LAUNCH_BLOCKER_WWMI_RESOURCE_TIER = "WWMI_RESOURCE_TIER_DECISION_REQUIRED";
const WWMI_RESOURCE_TIERS = ["UHD", "HD", "SD"] as const;
// Keep in sync with errD3D11ModeNoticeRequired in internal/xxmi/launch_builtin.go.
const LAUNCH_BLOCKER_D3D11_MODE = "XXMI_D3D11_MODE_NOTICE_REQUIRED";
// Keep in sync with errLoggingEnabled in internal/xxmi/launch_guard.go.
const LAUNCH_BLOCKER_LOGGING = "XXMI_LOGGING_ENABLED";
const LAUNCH_BLOCKER_GAME_FOLDER = "XXMI_GAME_FOLDER_NOT_CONFIGURED";
const LAUNCH_BLOCKER_RUNTIME = "XXMI_RUNTIME_CORRUPTED";
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
  "XXMI_PLATFORM_NOT_FOUND",
  "XXMI_PLATFORM_GAME_NOT_FOUND",
  "XXMI_PLATFORM_OPTIONS_FAILED",
  "GIMI_FPS_UNLOCKER_RUNNING",
  "GIMI_FPS_UNLOCKER_CONFIG_FAILED",
  "GIMI_HDR_CONFIG_FAILED",
  "SRMI_FPS_UNLOCK_FAILED",
  "HIMI_FPS_UNLOCK_FAILED",
  "ZZMI_GAME_CONFIG_FAILED",
  "WWMI_GAME_CONFIG_FAILED",
  "RESHADE_NOT_INSTALLED",
] as const;
// Package IDs and effect file names the user declined at launch, which are not brought up again
// until the app restarts.
const declinedEffects = new Set<string>();

type LaunchDialog =
  | "gimi-dcr"
  | "smooth-motion"
  | "launch-blockers"
  | "wwmi-wounded"
  | "wwmi-resource-tier"
  | "d3d11-mode"
  | "xxmi-logging"
  | "game-folder"
  | "runtime-repair";

export type LaunchGuardResult =
  | { status: "started" }
  | {
      status: "blocked";
      kind: LaunchDialog | "importer-setup" | "launch-error" | "update" | "preset-effects";
    };

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
  if (message.includes(LAUNCH_BLOCKER_WWMI_RESOURCE_TIER)) {
    return "wwmi-resource-tier";
  }
  if (message.includes(LAUNCH_BLOCKER_D3D11_MODE)) {
    return "d3d11-mode";
  }
  if (message.includes(LAUNCH_BLOCKER_LOGGING)) {
    return "xxmi-logging";
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
  // Steam and Epic Games installs ship HD, and it is the official launcher's usual default.
  const [resourceTier, setResourceTier] = useState<string>("HD");
  const [detectedFolders, setDetectedFolders] = useState<
    Awaited<ReturnType<typeof XXMI.DetectGameFolders>> | undefined
  >();
  const confirmGeneration = useRef(0);
  // The user chose to launch with logging on; later dialogs of the same launch must not ask again.
  const keepLogging = useRef(false);
  const queryClient = useQueryClient();
  const [pendingUpdate, setPendingUpdate] = useState<{
    importer: string;
    updates: UpdateStatus[];
  } | null>(null);
  const [pendingEffects, setPendingEffects] = useState<{
    importer: string;
    effects: PresetEffects;
  } | null>(null);

  const launch = useCallback(
    async (importer: string, withLogging = false): Promise<LaunchGuardResult> => {
      keepLogging.current = withLogging;
      try {
        await (withLogging ? XXMI.StartGameWithLogging(importer) : XXMI.StartGame(importer));
        return { status: "started" };
      } catch (error) {
        const message = toErrorMessage(error);
        if (
          message.includes("XXMI_NOT_CONFIGURED") ||
          message.includes("XXMI_IMPORTER_NOT_INSTALLED")
        ) {
          toast.info(t("page.setting.xxmi.builtin.importerSetupRequired", { importer }));
          await navigate({ to: "/xxmi/$importer", params: { importer } });
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

  const launchWithEffects = useCallback(
    async (importer: string): Promise<LaunchGuardResult> => {
      // A failed check must not keep the game from launching.
      const effects = await XXMI.LaunchPresetEffects(importer).catch(() => null);
      if (
        effects?.packages?.some((pkg) => !declinedEffects.has(pkg.id)) ||
        effects?.unknown?.some((file) => !declinedEffects.has(file))
      ) {
        setPendingEffects({ importer, effects });
        return { status: "blocked", kind: "preset-effects" };
      }
      return launch(importer);
    },
    [launch],
  );

  const startImporter = useCallback(
    async (importer: string): Promise<LaunchGuardResult> => {
      // A failed update check must not keep the game from launching.
      const updates = await XXMI.LaunchUpdates(importer).catch(() => null);
      if (updates?.length) {
        setPendingUpdate({ importer, updates });
        return { status: "blocked", kind: "update" };
      }
      return launchWithEffects(importer);
    },
    [launchWithEffects],
  );

  const launchPendingEffects = useCallback(
    async (declined: boolean) => {
      if (!pendingEffects) return;
      if (declined) {
        pendingEffects.effects.packages?.forEach((pkg) => declinedEffects.add(pkg.id));
        pendingEffects.effects.unknown?.forEach((file) => declinedEffects.add(file));
      }
      setPendingEffects(null);
      try {
        await launch(pendingEffects.importer);
      } catch (error) {
        toast.error(toErrorMessage(error));
      }
    },
    [launch, pendingEffects],
  );

  const handleUpdateConfirm = useCallback(async () => {
    if (!pendingUpdate) return;
    const { importer, updates } = pendingUpdate;

    // Like the automatic update, a failed install falls back to launching with the current files.
    try {
      await XXMI.InstallUpdates(
        importer,
        updates.map((entry) => entry.package),
      );
    } catch (error) {
      toast.warning(t("page.setting.xxmi.builtin.launchUpdateFailed", { importer }), {
        description: toErrorMessage(error),
      });
    }
    setPendingUpdate(null);
    void queryClient.invalidateQueries({
      predicate: (query) =>
        typeof query.queryKey[0] === "string" && query.queryKey[0].startsWith("xxmi:"),
    });

    try {
      await launchWithEffects(importer);
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  }, [launchWithEffects, pendingUpdate, queryClient, t]);

  // Bumping the generation cancels any in-flight confirmation, so dismissing the dialog
  // while ClearLaunchBlockers is still running does not launch the game afterwards.
  const closeDialog = useCallback(() => {
    confirmGeneration.current += 1;
    setIsConfirming(false);
    setPendingImporter(null);
    setRuntimeError(null);
    setGameFolder("");
    setResourceTier("HD");
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
      } else if (dialog === "wwmi-resource-tier") {
        const config = await XXMI.GetImporterConfig(importer);
        if (!config.wwmi) throw new Error("WWMI settings are unavailable");
        await XXMI.SaveImporterConfig(importer, {
          ...config,
          wwmi: { ...config.wwmi, resourceTier, resourceTierDecided: true },
        });
      } else if (dialog === "d3d11-mode") {
        const config = await XXMI.GetImporterConfig(importer);
        await XXMI.SaveImporterConfig(importer, { ...config, d3d11ModeNoticeShown: true });
      } else if (dialog === "xxmi-logging") {
        await XXMI.DisableLogging(importer);
        void queryClient.invalidateQueries({ queryKey: ["xxmi:config", importer] });
      } else {
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
    // The resource quality, the DirectX 11 reminder, and logging are saved answers that cannot be
    // asked twice, and the launch may still need another question, so those go through the guard again.
    try {
      if (dialog === "wwmi-resource-tier" || dialog === "d3d11-mode" || dialog === "xxmi-logging") {
        await launch(importer, keepLogging.current);
      } else if (keepLogging.current) {
        await XXMI.StartGameWithLogging(importer);
      } else {
        await XXMI.StartGame(importer);
      }
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  }, [dialog, gameFolder, launch, pendingImporter, queryClient, resourceTier]);

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
      await (keepLogging.current ? XXMI.StartGameWithLogging(importer) : XXMI.StartGame(importer));
    } catch (error) {
      if (confirmGeneration.current === generation) toast.error(toErrorMessage(error));
    } finally {
      if (confirmGeneration.current === generation) setIsConfirming(false);
    }
  }, [pendingImporter]);

  // Logging stays as configured, and the launch may still need another question.
  const handleKeepLogging = useCallback(async () => {
    if (!pendingImporter) return;
    const importer = pendingImporter;
    confirmGeneration.current += 1;
    setPendingImporter(null);
    try {
      await launch(importer, true);
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  }, [launch, pendingImporter]);

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
                  : dialog === "runtime-repair"
                    ? "page.setting.xxmi.builtin.repairRuntime"
                    : `page.mod.dialog.${dialog}.title`,
              )}
            </AlertDialogTitle>
            {dialog !== "game-folder" && (
              <AlertDialogDescription>
                {t(
                  dialog === "runtime-repair"
                    ? "page.setting.xxmi.builtin.runtimeRepairPrompt"
                    : `page.mod.dialog.${dialog}.description`,
                  { importer: pendingImporter },
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
          {dialog === "wwmi-resource-tier" && (
            <ButtonGroup>
              {WWMI_RESOURCE_TIERS.map((tier) => (
                <Button
                  key={tier}
                  variant={resourceTier === tier ? "default" : "outline"}
                  aria-pressed={resourceTier === tier}
                  disabled={isConfirming}
                  onClick={() => setResourceTier(tier)}
                >
                  {tier}
                </Button>
              ))}
            </ButtonGroup>
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
            {dialog === "xxmi-logging" && (
              <AlertDialogAction
                variant="outline"
                disabled={isConfirming}
                onClickPromise={handleKeepLogging}
              >
                {t("page.mod.dialog.xxmi-logging.keep")}
              </AlertDialogAction>
            )}
            <AlertDialogAction
              disabled={isConfirming || (dialog === "game-folder" && !gameFolder.trim())}
              onClickPromise={handleConfirm}
            >
              {t(
                dialog === "game-folder"
                  ? "g.save"
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
      handleKeepLogging,
      handleKeepWounded,
      isConfirming,
      pendingImporter,
      resourceTier,
      runtimeError,
      t,
    ],
  );

  return {
    startImporter,
    launchGuardDialog: (
      <>
        {alert}
        <XXMIUpdateDialog
          importer={pendingUpdate?.importer ?? null}
          updates={pendingUpdate?.updates ?? []}
          confirmLabel={t("page.setting.xxmi.builtin.launchUpdateConfirm")}
          onConfirm={handleUpdateConfirm}
          onClose={() => setPendingUpdate(null)}
        />
        <ReShadePresetEffectsDialog
          effects={pendingEffects?.effects ?? null}
          confirmLabel={t("page.setting.xxmi.builtin.reshade.presetEffectsInstallAndLaunch")}
          skipLabel={t(
            pendingEffects?.effects.packages?.length
              ? "page.setting.xxmi.builtin.reshade.presetEffectsSkipLaunch"
              : "page.setting.xxmi.builtin.reshade.presetEffectsIgnoreLaunch",
          )}
          onInstalled={() => launchPendingEffects(false)}
          onSkip={() => launchPendingEffects(true)}
          onClose={() => setPendingEffects(null)}
        />
      </>
    ),
  };
}
