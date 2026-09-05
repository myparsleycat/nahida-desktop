import {
  Tools,
  type ZZMIBackupSession,
  type ZZMIFixerPrepareResult,
  type ZZMIFixerRestoreConflict,
} from "@bindings/tools";
import { Logger } from "@renderer/lib/logger";
import { useModStore } from "@renderer/store/mod";
import { getFixToolPresets, getFixToolScripts } from "@renderer/wails/fix-tools";
import {
  type GitHubRateState,
  type WuwaBackupGroup,
  type WuwaBackupSize,
  type WuwaFixerOptions,
  type WuwaFixerPrepareResult,
  type FixToolLogEvent,
} from "@shared/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { getModFixerAvailability } from "./mod-fixer-action";
import { useGames } from "./use-mod-data";

const defaultWuwaOptions = (): WuwaFixerOptions => ({
  derivedHashes: false,
  stableTexture: false,
  aemeathMech: false,
  rendering33: false,
  aeroFix: "none",
});

export function useModFixRunner() {
  const { t } = useTranslation();
  const selectedGame = useModStore((s) => s.selectedGame);
  const { data: games = [] } = useGames();
  const selectedGameConfig = games.find((game) => game.game === selectedGame) ?? null;
  const selectedImporter = selectedGameConfig?.importer ?? null;
  const { showWuwaFixer, showZZMIFixer, modFixer } = getModFixerAvailability(selectedImporter, t);

  const { data: fixTools = [] } = useQuery({
    queryKey: ["ftm:scripts"],
    queryFn: getFixToolScripts,
  });

  const { data: presets = [] } = useQuery({
    queryKey: ["ftm:presets"],
    queryFn: getFixToolPresets,
  });

  const [activeModPath, setActiveModPath] = useState<string | null>(null);
  const [showLogModal, setShowLogModal] = useState(false);
  const [showInstallDialog, setShowInstallDialog] = useState(false);
  const [showUpdateDialog, setShowUpdateDialog] = useState(false);
  const [showOptionsDialog, setShowOptionsDialog] = useState(false);
  const [optionsTab, setOptionsTab] = useState<"fix" | "rollback">("fix");
  const [logs, setLogs] = useState<string[]>([]);
  const queryClient = useQueryClient();
  const [isRunning, setIsRunning] = useState(false);
  const [activeRunKind, setActiveRunKind] = useState<"script" | "wuwa" | "zzmi">("script");
  const [isPreparing, setIsPreparing] = useState(false);
  const [inputCmd, setInputCmd] = useState("");
  const [prepareResult, setPrepareResult] = useState<WuwaFixerPrepareResult | null>(null);
  const [wuwaOptions, setWuwaOptions] = useState<WuwaFixerOptions>(defaultWuwaOptions);
  const [isRollbackBusy, setIsRollbackBusy] = useState(false);
  const [pendingRollbackKey, setPendingRollbackKey] = useState<string | null>(null);
  const [showAdvancedRollback, setShowAdvancedRollback] = useState(false);
  const [showCleanConfirm, setShowCleanConfirm] = useState(false);
  const [cleanConfirmInput, setCleanConfirmInput] = useState("");
  const runInProgressRef = useRef(false);
  const translationKey = "page.mod.dialog.wuwa-fix-runner";

  const [showZZMIDialog, setShowZZMIDialog] = useState(false);
  const [zzmiTab, setZZMITab] = useState<"hash" | "jane" | "dialyn" | "rollback">("hash");
  const [zzmiPrepare, setZZMIPrepare] = useState<ZZMIFixerPrepareResult | null>(null);
  const [zzmiUpdateBusy, setZZMIUpdateBusy] = useState(false);
  const [zzmiRollbackBusy, setZZMIRollbackBusy] = useState(false);
  const [zzmiConflicts, setZZMIConflicts] = useState<ZZMIFixerRestoreConflict[]>([]);
  const [zzmiPendingRestore, setZZMIPendingRestore] = useState<{
    sessionId: string;
    entryId?: string;
  } | null>(null);
  const [showZZMICleanConfirm, setShowZZMICleanConfirm] = useState(false);

  const { data: backupsData, isLoading: isLoadingBackups } = useQuery({
    queryKey: ["wuwaFixer:backups", activeModPath],
    queryFn: async () => {
      if (!activeModPath) {
        return {
          groups: [] as WuwaBackupGroup[],
          size: { bytes: 0, count: 0 } as WuwaBackupSize,
        };
      }
      const [groups, size] = await Promise.all([
        Tools.WuwaFixerScanBackups(activeModPath),
        Tools.WuwaFixerGetBackupSize(activeModPath),
      ]);
      return { groups, size };
    },
    enabled: showOptionsDialog && optionsTab === "rollback" && !!activeModPath,
  });

  const backupGroups = backupsData?.groups ?? [];
  const backupSize = backupsData?.size ?? { bytes: 0, count: 0 };

  const refreshBackups = useCallback(
    async (modPath = activeModPath) => {
      if (!modPath) return;
      await queryClient.invalidateQueries({ queryKey: ["wuwaFixer:backups", modPath] });
    },
    [activeModPath, queryClient],
  );

  const { data: zzmiBackups = [], isLoading: isLoadingZZMIBackups } = useQuery({
    queryKey: ["zzmiFixer:backups", activeModPath],
    queryFn: async () => {
      if (!activeModPath) return [] as ZZMIBackupSession[];
      return (await Tools.ZZMIFixerListBackups(activeModPath)) ?? [];
    },
    enabled: showZZMIDialog && zzmiTab === "rollback" && !!activeModPath,
  });

  const refreshZZMIBackups = useCallback(
    async (modPath = activeModPath) => {
      if (!modPath) return;
      await queryClient.invalidateQueries({ queryKey: ["zzmiFixer:backups", modPath] });
    },
    [activeModPath, queryClient],
  );

  useEffect(() => {
    if (!showLogModal) return;
    const off = Events.On("ftm:log", (event) => {
      const payload = event.data as FixToolLogEvent;
      setLogs((prev) => {
        if (payload.replaceLast && prev.length > 0) {
          return [...prev.slice(0, -1), payload.message];
        }

        return [...prev, payload.message];
      });
    });
    return off;
  }, [showLogModal]);

  const handleRun = async (type: "tool" | "preset", id: string, modPath: string) => {
    if (runInProgressRef.current) {
      return;
    }

    runInProgressRef.current = true;
    setActiveModPath(modPath);
    setActiveRunKind("script");
    setShowLogModal(true);
    setLogs([]);
    setIsRunning(true);
    try {
      if (type === "tool") {
        await Tools.RunScript(id, modPath);
      } else {
        await Tools.RunPreset(id, modPath);
      }
    } catch (error) {
      Logger.capture("hooks/use-mod-fix-runner.tsx", error);
    } finally {
      setIsRunning(false);
      runInProgressRef.current = false;
    }
  };

  const handleCancel = () => {
    void Tools.CancelRun();
  };

  const handleSendInput = () => {
    if (!isRunning) {
      return;
    }

    void Tools.SendInput(`${inputCmd}\r\n`);
    setInputCmd("");
  };

  const resetWuwaDialogs = () => {
    setShowInstallDialog(false);
    setShowUpdateDialog(false);
    setShowOptionsDialog(false);
  };

  const openOptionsDialog = () => {
    setWuwaOptions(defaultWuwaOptions());
    setOptionsTab("fix");
    setPendingRollbackKey(null);
    setShowAdvancedRollback(false);
    setShowCleanConfirm(false);
    setCleanConfirmInput("");
    setShowOptionsDialog(true);
  };

  const handleOpenWuwaFixer = async (modPath: string) => {
    if (!showWuwaFixer) {
      toast.error("Wuwa Mod Fixer is only available for WWMI or importer-less games.");
      return;
    }

    setActiveModPath(modPath);
    setIsPreparing(true);
    try {
      const result = await Tools.WuwaFixerPrepareRun(selectedImporter);
      setPrepareResult(result);

      if (!result.supported) {
        toast.error("This game does not support Wuwa Mod Fixer.");
        return;
      }

      if (!result.installed) {
        setShowInstallDialog(true);
        return;
      }

      if (result.updateAvailable) {
        setShowUpdateDialog(true);
        return;
      }

      openOptionsDialog();
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setIsPreparing(false);
    }
  };

  const installOrUpdateAndContinue = async () => {
    setIsPreparing(true);
    try {
      await Tools.WuwaFixerInstallOrUpdate();
      const nextResult = await Tools.WuwaFixerPrepareRun(selectedImporter);
      setPrepareResult(nextResult);
      resetWuwaDialogs();
      openOptionsDialog();
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setIsPreparing(false);
    }
  };

  const handleRunWuwaFixer = async () => {
    if (!activeModPath) {
      return;
    }

    setActiveRunKind("wuwa");
    setShowLogModal(true);
    setLogs([]);
    setIsRunning(true);
    setShowOptionsDialog(false);
    try {
      await Tools.WuwaFixerRun(activeModPath, wuwaOptions);
    } catch (error) {
      Logger.capture("hooks/use-mod-fix-runner.tsx", error);
    } finally {
      setIsRunning(false);
    }
  };

  const prepareZZMI = async (modPath: string, forceRefresh: boolean) => {
    const result = await Tools.ZZMIFixerPrepare(modPath, forceRefresh);
    setZZMIPrepare(result);
    return result;
  };

  const handleOpenZZMIFixer = async (modPath: string) => {
    if (!showZZMIFixer) {
      toast.error(t("page.mod.dialog.zzmi-fix-runner.only_zzmi"));
      return;
    }

    setActiveModPath(modPath);
    setIsPreparing(true);
    setZZMITab("hash");
    setZZMIConflicts([]);
    try {
      await prepareZZMI(modPath, false);
      setShowZZMIDialog(true);
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setIsPreparing(false);
    }
  };

  const handleRefreshZZMIRules = async () => {
    if (!activeModPath) return;
    setZZMIUpdateBusy(true);
    try {
      await prepareZZMI(activeModPath, true);
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setZZMIUpdateBusy(false);
    }
  };

  const handleUpdateZZMIRules = async () => {
    if (!activeModPath) return;
    setZZMIUpdateBusy(true);
    try {
      await Tools.ZZMIFixerActivateLatestRules();
      await prepareZZMI(activeModPath, false);
      toast.success(t("page.mod.dialog.zzmi-fix-runner.update.success"));
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setZZMIUpdateBusy(false);
    }
  };

  const handleRunZZMIFixer = async (tool: "hash" | "jane" | "dialyn") => {
    if (!activeModPath || runInProgressRef.current || zzmiUpdateBusy) return;
    runInProgressRef.current = true;
    setActiveRunKind("zzmi");
    setShowZZMIDialog(false);
    setShowLogModal(true);
    setLogs([]);
    setIsRunning(true);
    try {
      const result = await Tools.ZZMIFixerRun({ path: activeModPath, tool });
      if (result.sessionId) {
        await refreshZZMIBackups(activeModPath);
      }
      if ((result.warnings?.length ?? 0) > 0) {
        toast.warning(
          t("page.mod.dialog.zzmi-fix-runner.run.warning", {
            count: result.warnings?.length ?? 0,
          }),
        );
      }
    } catch (error) {
      Logger.capture("hooks/use-mod-fix-runner.tsx", error);
      toast.error((error as Error).message);
    } finally {
      setIsRunning(false);
      runInProgressRef.current = false;
    }
  };

  const restoreZZMI = async (sessionId: string, entryId?: string, force = false) => {
    if (!activeModPath || zzmiRollbackBusy) return;
    setZZMIRollbackBusy(true);
    try {
      const result = await Tools.ZZMIFixerRestore({
        path: activeModPath,
        sessionId,
        entryId,
        force,
      });
      if ((result.conflicts?.length ?? 0) > 0) {
        setZZMIConflicts(result.conflicts ?? []);
        setZZMIPendingRestore({ sessionId, entryId });
        await refreshZZMIBackups(activeModPath);
        return;
      }
      setZZMIConflicts([]);
      setZZMIPendingRestore(null);
      toast.success(t("page.mod.dialog.zzmi-fix-runner.rollback.success"));
      await refreshZZMIBackups(activeModPath);
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setZZMIRollbackBusy(false);
    }
  };

  const handleDeleteZZMIBackup = async (sessionId: string, entryId?: string) => {
    if (!activeModPath || zzmiRollbackBusy) return;
    setZZMIRollbackBusy(true);
    try {
      await Tools.ZZMIFixerDeleteBackup({ path: activeModPath, sessionId, entryId });
      await refreshZZMIBackups(activeModPath);
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setZZMIRollbackBusy(false);
    }
  };

  const handleDeleteAllZZMIBackups = async () => {
    if (!activeModPath || zzmiRollbackBusy) return;
    setZZMIRollbackBusy(true);
    try {
      await Tools.ZZMIFixerDeleteAllBackups(activeModPath);
      setShowZZMICleanConfirm(false);
      await refreshZZMIBackups(activeModPath);
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setZZMIRollbackBusy(false);
    }
  };

  const setOptionFlag = (
    key: "derivedHashes" | "stableTexture" | "aemeathMech" | "rendering33",
    checked: boolean,
  ) => {
    setWuwaOptions((prev) => {
      const next = { ...prev, [key]: checked };

      if (key === "derivedHashes" && checked) {
        next.stableTexture = false;
      }
      if (key === "stableTexture" && checked) {
        next.derivedHashes = false;
      }

      return next;
    });
  };

  const toggleAeroFix = () => {
    setWuwaOptions((prev) => ({
      ...prev,
      aeroFix: prev.aeroFix === "none" ? "1" : "none",
    }));
  };

  const setAeroFixMode = (mode: "1" | "2") => {
    setWuwaOptions((prev) => ({
      ...prev,
      aeroFix: mode,
    }));
  };

  const handleRollbackToGroup = async (groupKey: string) => {
    if (!activeModPath || isRollbackBusy) {
      return;
    }

    const targetKey =
      groupKey === "__RESTORE_ALL__" && backupGroups.length > 0
        ? backupGroups[backupGroups.length - 1].groupKey
        : groupKey;

    setIsRollbackBusy(true);
    try {
      await Tools.WuwaFixerRollbackToGroup(activeModPath, targetKey);
      toast.success(t(`${translationKey}.rollback.success`));
      await refreshBackups(activeModPath);
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setIsRollbackBusy(false);
      setPendingRollbackKey(null);
    }
  };

  const handleCleanBackups = async () => {
    if (!activeModPath || isRollbackBusy || cleanConfirmInput.toUpperCase() !== "WIPE") {
      return;
    }

    setIsRollbackBusy(true);
    try {
      await Tools.WuwaFixerCleanBackups(activeModPath);
      toast.success(t(`${translationKey}.rollback.clean_success`));
      await refreshBackups(activeModPath);
    } catch (error) {
      toast.error((error as Error).message);
    } finally {
      setIsRollbackBusy(false);
      setShowCleanConfirm(false);
      setCleanConfirmInput("");
    }
  };

  const isRateLimited = prepareResult?.rateLimited ?? false;
  const rateResetText = formatRateResetText(prepareResult?.rateState ?? null);
  return {
    fixTools,
    presets,
    showWuwaFixer,
    showZZMIFixer,
    modFixer,
    activeModPath,
    selectedImporter,
    showLogModal,
    setShowLogModal,
    showInstallDialog,
    setShowInstallDialog,
    showUpdateDialog,
    setShowUpdateDialog,
    showOptionsDialog,
    setShowOptionsDialog,
    optionsTab,
    setOptionsTab,
    logs,
    isRunning,
    activeRunKind,
    isPreparing,
    inputCmd,
    setInputCmd,
    prepareResult,
    wuwaOptions,
    setWuwaOptions,
    backupGroups,
    backupSize,
    isLoadingBackups,
    isRollbackBusy,
    pendingRollbackKey,
    setPendingRollbackKey,
    showAdvancedRollback,
    setShowAdvancedRollback,
    showCleanConfirm,
    setShowCleanConfirm,
    cleanConfirmInput,
    setCleanConfirmInput,
    isRateLimited,
    rateResetText,
    handleRun,
    handleCancel,
    handleSendInput,
    handleOpenWuwaFixer,
    handleOpenZZMIFixer,
    handleOpenModFixer: showZZMIFixer ? handleOpenZZMIFixer : handleOpenWuwaFixer,
    handleInstallAndContinue: installOrUpdateAndContinue,
    handleUpdateAndContinue: installOrUpdateAndContinue,
    handleProceedWithoutUpdate: () => {
      setShowUpdateDialog(false);
      openOptionsDialog();
    },
    handleRunWuwaFixer,
    setOptionFlag,
    toggleAeroFix,
    setAeroFixMode,
    refreshBackups,
    handleRollbackToGroup,
    handleCleanBackups,
    showZZMIDialog,
    setShowZZMIDialog,
    zzmiTab,
    setZZMITab,
    zzmiPrepare,
    zzmiUpdateBusy,
    zzmiBackups,
    isLoadingZZMIBackups,
    refreshZZMIBackups,
    zzmiRollbackBusy,
    zzmiConflicts,
    zzmiPendingRestore,
    setZZMIConflicts,
    setZZMIPendingRestore,
    showZZMICleanConfirm,
    setShowZZMICleanConfirm,
    handleRefreshZZMIRules,
    handleUpdateZZMIRules,
    handleRunZZMIFixer,
    restoreZZMI,
    handleDeleteZZMIBackup,
    handleDeleteAllZZMIBackups,
    labels: {
      logTitle: t("page.mod.log-dialog.title"),
    },
  };
}

function formatRateResetText(rateState: GitHubRateState | null) {
  if (!rateState) {
    return null;
  }

  return new Date(rateState.reset * 1000).toLocaleString();
}
