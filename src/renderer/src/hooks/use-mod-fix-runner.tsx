import { useModStore } from "@renderer/store/mod";
import {
  type GitHubRateState,
  type WuwaFixerOptions,
  type WuwaFixerPrepareResult,
  type FixToolLogEvent,
} from "@shared/types";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { useGames } from "./use-mod-data";

const defaultWuwaOptions = (): WuwaFixerOptions => ({
  derivedHashes: false,
  stableTexture: false,
  aemeathMech: false,
  aeroFix: "none",
  rollback: false,
});

export function useModFixRunner() {
  const { t } = useTranslation();
  const selectedGame = useModStore((s) => s.selectedGame);
  const { data: games = [] } = useGames();
  const selectedGameConfig = games.find((game) => game.game === selectedGame) ?? null;
  const selectedImporter = selectedGameConfig?.importer ?? null;
  const showWuwaFixer = selectedImporter === null || selectedImporter?.toUpperCase() === "WWMI";

  const { data: fixTools = [] } = useQuery({
    queryKey: ["ftm:scripts"],
    queryFn: () => window.api.invoke("ftm:getScripts"),
  });

  const { data: presets = [] } = useQuery({
    queryKey: ["ftm:presets"],
    queryFn: () => window.api.invoke("ftm:getPresets"),
  });

  const [activeModPath, setActiveModPath] = useState<string | null>(null);
  const [showLogModal, setShowLogModal] = useState(false);
  const [showInstallDialog, setShowInstallDialog] = useState(false);
  const [showUpdateDialog, setShowUpdateDialog] = useState(false);
  const [showOptionsDialog, setShowOptionsDialog] = useState(false);
  const [logs, setLogs] = useState<string[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [isPreparing, setIsPreparing] = useState(false);
  const [inputCmd, setInputCmd] = useState("");
  const [prepareResult, setPrepareResult] = useState<WuwaFixerPrepareResult | null>(null);
  const [wuwaOptions, setWuwaOptions] = useState<WuwaFixerOptions>(defaultWuwaOptions);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!showLogModal) return;
    const removeListener = window.api.on("ftm:log", (event: FixToolLogEvent) => {
      setLogs((prev) => {
        if (event.replaceLast && prev.length > 0) {
          return [...prev.slice(0, -1), event.message];
        }

        return [...prev, event.message];
      });
    });
    return () => removeListener();
  }, [showLogModal]);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs]);

  const handleRun = async (type: "tool" | "preset", id: string, modPath: string) => {
    setActiveModPath(modPath);
    setShowLogModal(true);
    setLogs([]);
    setIsRunning(true);
    try {
      if (type === "tool") {
        await window.api.invoke("ftm:runScript", id, modPath);
      } else {
        await window.api.invoke("ftm:runPreset", id, modPath);
      }
    } catch (error) {
      console.error(error);
    } finally {
      setIsRunning(false);
    }
  };

  const handleCancel = () => {
    void window.api.invoke("ftm:cancelRun");
  };

  const handleSendInput = () => {
    if (!isRunning) {
      return;
    }

    void window.api.invoke("ftm:sendInput", `${inputCmd}\r\n`);
    setInputCmd("");
  };

  const resetWuwaDialogs = () => {
    setShowInstallDialog(false);
    setShowUpdateDialog(false);
    setShowOptionsDialog(false);
  };

  const openOptionsDialog = () => {
    setWuwaOptions(defaultWuwaOptions());
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
      const result = await window.api.invoke("wuwaFixer:prepareRun", selectedImporter);
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
      await window.api.invoke("wuwaFixer:installOrUpdate");
      const nextResult = await window.api.invoke("wuwaFixer:prepareRun", selectedImporter);
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

    setShowLogModal(true);
    setLogs([]);
    setIsRunning(true);
    setShowOptionsDialog(false);
    try {
      await window.api.invoke("wuwaFixer:run", activeModPath, wuwaOptions);
    } catch (error) {
      console.error(error);
    } finally {
      setIsRunning(false);
    }
  };

  const setCheckbox = (key: keyof WuwaFixerOptions, checked: boolean) => {
    setWuwaOptions((prev) => {
      const next = { ...prev, [key]: checked };

      if (key === "derivedHashes" && checked) {
        next.stableTexture = false;
      }
      if (key === "stableTexture" && checked) {
        next.derivedHashes = false;
      }
      if (key === "rollback" && checked) {
        next.derivedHashes = false;
        next.stableTexture = false;
        next.aemeathMech = false;
        next.aeroFix = "none";
      }

      return next;
    });
  };

  const isRateLimited = prepareResult?.rateLimited ?? false;
  const rateResetText = formatRateResetText(prepareResult?.rateState ?? null);

  return {
    fixTools,
    presets,
    showWuwaFixer,
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
    logs,
    isRunning,
    isPreparing,
    inputCmd,
    setInputCmd,
    inputRef,
    scrollRef,
    prepareResult,
    wuwaOptions,
    setWuwaOptions,
    isRateLimited,
    rateResetText,
    handleRun,
    handleCancel,
    handleSendInput,
    handleOpenWuwaFixer,
    handleInstallAndContinue: installOrUpdateAndContinue,
    handleUpdateAndContinue: installOrUpdateAndContinue,
    handleProceedWithoutUpdate: () => {
      setShowUpdateDialog(false);
      openOptionsDialog();
    },
    handleRunWuwaFixer,
    setCheckbox,
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
