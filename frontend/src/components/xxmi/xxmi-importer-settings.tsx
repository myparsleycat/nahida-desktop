import { Mod } from "@bindings/mod";
import type { GameConfig } from "@bindings/mod/models";
import { XXMI } from "@bindings/xxmi";
import { RuntimeMode, type CustomDLL, type ImporterConfig } from "@bindings/xxmi/models";
import { GameIcon } from "@renderer/components/game-icon";
import { Alert, AlertDescription } from "@renderer/components/ui/alert";
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
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { ButtonGroup } from "@renderer/components/ui/button-group";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import {
  Section,
  SectionContent,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import { Switch } from "@renderer/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@renderer/components/ui/tabs";
import { WWMIGraphicsSettings } from "@renderer/components/xxmi/wwmi-graphics-settings";
import { CUSTOM_DLL_SOURCE, CustomDLLField } from "@renderer/components/xxmi/xxmi-custom-dll";
import {
  FOLLOW_LATEST,
  NumberRow,
  PathField,
  SelectRow,
  ToggleRow,
} from "@renderer/components/xxmi/xxmi-fields";
import { importerFolderErrorCode } from "@renderer/components/xxmi/xxmi-importer-folder-error";
import { useLaunchGuard } from "@renderer/hooks/use-launch-guard";
import { cn } from "@renderer/lib/utils";
import { FileDropTargetID } from "@renderer/wails/file-drop";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useBlocker, useNavigate } from "@tanstack/react-router";
import { isEqual } from "es-toolkit";
import {
  CheckIcon,
  FileTextIcon,
  PlayIcon,
  ScanSearchIcon,
  ShieldAlertIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

type PendingImporterFolderChange = {
  next: ImporterConfig;
  games: GameConfig[];
  install?: { version: string; allowUnsigned: boolean };
};

// Offered beside the libraries providers and versions, whose names it must not collide with.
const FOLLOW_SHARED = "__shared__";

function normalizedFolder(folder: string) {
  return folder.replaceAll("/", "\\").replace(/\\+$/, "").toLowerCase();
}

async function linkedGamesForImporterMove(previousFolder: string | undefined, nextFolder: string) {
  const previous = previousFolder ? normalizedFolder(previousFolder) : "";
  const changed = normalizedFolder(nextFolder);
  if (!previous || previous === changed) return [];
  const oldMods = `${previous}\\mods`;
  return ((await Mod.GetGames()) ?? []).filter(
    (game) => normalizedFolder(game.modFolderPath) === oldMods,
  );
}

async function resolveImporterGameFolder(importer: string, next: ImporterConfig) {
  if (!next.gameFolder) return next;
  return {
    ...next,
    gameFolder: (await XXMI.ValidateGameFolder(importer, next.gameFolder)).path,
  };
}

export function XXMIImporterSettings({ importer }: { importer: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { startImporter, launchGuardDialog } = useLaunchGuard();
  const { data: saved } = useQuery({
    queryKey: ["xxmi:config", importer],
    queryFn: () => XXMI.GetImporterConfig(importer),
  });
  const { data: releases } = useQuery({
    queryKey: ["xxmi:releases", importer],
    queryFn: () => XXMI.ListReleases(`importer:${importer}`),
    staleTime: 60 * 60 * 1000,
    retry: false,
  });
  const { data: packageVerification } = useQuery({
    queryKey: ["xxmi:package-verification", importer],
    queryFn: () => XXMI.GetImporterPackageVerification(importer),
    enabled: !!saved,
  });
  const { data: cachedLibs } = useQuery({
    queryKey: ["xxmi:libs-cache"],
    queryFn: XXMI.ListCachedLibs,
  });
  const { data: overview } = useQuery({ queryKey: ["xxmi:overview"], queryFn: XXMI.GetOverview });
  const customDll = !!overview?.importers?.find((entry) => entry.key === importer)?.customDll;
  const sharedLibsVersion = overview?.sharedLibsVersion;
  const libsProviders = overview?.libsProviders ?? [];
  const [draft, setConfig] = useState<ImporterConfig | null>(null);
  const config = draft ?? saved ?? null;
  const dirty = draft !== null && !isEqual(draft, saved);
  // A legacy runtime and a user-provided DLL deploy the default provider's signed libraries, so that is the
  // provider whose releases their libraries version names.
  // The importer's own DLL comes first; the shared one reaches importers that follow the shared provider.
  const selectedDll = (cfg: ImporterConfig) =>
    cfg.mode === RuntimeMode.RuntimeXXMI
      ? cfg.customDll || (cfg.libsProvider ? "" : (overview?.sharedCustomDll ?? ""))
      : "";
  const appliedCustomDll = (cfg: ImporterConfig) => (cfg.migoto.unsafeMode ? selectedDll(cfg) : "");
  const launchesCustomDll = (cfg: ImporterConfig) => {
    // The overview reports the saved config. A saved selection accounts for its custom DLL, so a draft that
    // drops the selection drops the DLL; only a DLL kept in the importer folder outlives the draft.
    const keptDll =
      customDll && cfg.migoto.unsafeMode && !(saved != null && appliedCustomDll(saved));
    return !!appliedCustomDll(cfg) || keptDll;
  };
  const deployedLibsProvider = (cfg: ImporterConfig | null | undefined) => {
    if (!cfg) return undefined;
    return cfg.mode === RuntimeMode.RuntimeXXMI && !launchesCustomDll(cfg)
      ? cfg.libsProvider || overview?.sharedLibsProvider
      : libsProviders[0];
  };
  const libsProvider = deployedLibsProvider(config);
  const { data: libsReleases } = useQuery({
    queryKey: ["xxmi:libs-releases", libsProvider],
    queryFn: () => XXMI.GetLibsProviderReleases(libsProvider ?? ""),
    enabled: !!libsProvider,
    staleTime: 60 * 60 * 1000,
    retry: false,
  });
  const { data: legacyRuntimes } = useQuery({
    queryKey: ["xxmi:legacy-cache"],
    queryFn: XXMI.GetLegacyRuntimes,
    enabled: config?.mode === RuntimeMode.RuntimeLegacy,
  });
  const [tab, setTab] = useState("general");
  const selectedPackage = config?.packageVersion.pinned ?? "";
  const [packageDialogVersion, setPackageDialogVersion] = useState<string | null>(null);
  const dialogRelease = releases?.find((release) => release.version === packageDialogVersion);
  const installedImporter = overview?.importers?.find((entry) => entry.key === importer);
  const installedVersion = installedImporter?.installedVersion ?? packageVerification?.version;
  const packageInstalledInFolder =
    !!installedVersion &&
    normalizedFolder(config?.importerFolder ?? "") ===
      normalizedFolder(installedImporter?.importerFolder ?? saved?.importerFolder ?? "");
  const isInstalledPackageVersion = (version: string) =>
    packageInstalledInFolder &&
    version.trim().replace(/^v/i, "") === installedVersion?.trim().replace(/^v/i, "");
  const packageNeedsInstall = !!selectedPackage && !isInstalledPackageVersion(selectedPackage);
  // A disabled importer never launches, so it can be saved, and so turned off, without its package.
  const saveBlocked = !!config?.enabled && packageNeedsInstall;
  const [isSaving, setIsSaving] = useState(false);
  // A picked DLL is only in the draft until the config is saved, so the overview does not list it yet.
  const [importedDll, setImportedDll] = useState<CustomDLL | null>(null);
  const [pendingCustomDll, setPendingCustomDll] = useState<CustomDLL | null>(null);
  // Choosing the custom DLL as the provider changes nothing in the draft until a file is picked.
  const [pickingCustomDll, setPickingCustomDll] = useState(false);
  const [allowUnsigned, setAllowUnsigned] = useState(false);
  const [optimizationPreview, setOptimizationPreview] = useState<
    Awaited<ReturnType<typeof XXMI.OptimizeMods>> | undefined
  >();
  const [detectedFolders, setDetectedFolders] = useState<
    Awaited<ReturnType<typeof XXMI.DetectGameFolders>> | undefined
  >();
  const [pendingFolderChange, setPendingFolderChange] =
    useState<PendingImporterFolderChange | null>(null);

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["xxmi:config", importer] });
    void queryClient.invalidateQueries({ queryKey: ["xxmi:package-verification", importer] });
    void queryClient.invalidateQueries({ queryKey: ["xxmi:overview"] });
    void queryClient.invalidateQueries({ queryKey: ["xxmi:updates"] });
  };
  const persist = async (
    next: ImporterConfig,
    games: GameConfig[] = [],
    install?: PendingImporterFolderChange["install"],
  ) => {
    setIsSaving(true);
    try {
      if (install) {
        await XXMI.InstallImporterPackage({ importer, ...install, config: next });
      } else {
        const pinChanged =
          !!next.xxmiVersion.pinned && next.xxmiVersion.pinned !== saved?.xxmiVersion.pinned;
        if (next.xxmiVersion.pinned && pinChanged) {
          await XXMI.EnsureLibsVersion(next.xxmiVersion.pinned);
        }
        const provider = deployedLibsProvider(next);
        if (provider && (pinChanged || provider !== deployedLibsProvider(saved))) {
          await XXMI.EnsureLibsProvider(
            provider,
            (next.xxmiVersion.follow === "shared" ? sharedLibsVersion : next.xxmiVersion.pinned) ??
              "",
          );
        }
        await XXMI.SaveImporterConfig(importer, next);
      }
      for (const game of games) {
        await Mod.UpdateGame(game.game, {
          modFolderPath: `${next.importerFolder}\\Mods`,
          importer: game.importer,
          linkedModFolderPath: game.linkedModFolderPath,
          gameInstallPath: game.gameInstallPath,
          gameExecutablePath: game.gameExecutablePath,
        });
      }
      setPendingFolderChange(null);
      setPackageDialogVersion(null);
      setConfig(null);
      setPickingCustomDll(false);
      refresh();
      return true;
    } catch (error) {
      const code = importerFolderErrorCode(error);
      toast.error(
        code
          ? t(`page.setting.xxmi.fn.installImporterPackage.errors.${code}`)
          : toErrorMessage(error),
      );
      return false;
    } finally {
      setIsSaving(false);
    }
  };
  const save = async (next = config) => {
    if (!next || isSaving) return;
    if (
      next.enabled &&
      next.packageVersion.pinned &&
      !isInstalledPackageVersion(next.packageVersion.pinned)
    )
      return;
    try {
      const resolved = await resolveImporterGameFolder(importer, next);
      const games = await linkedGamesForImporterMove(saved?.importerFolder, next.importerFolder);
      if (games.length) {
        setPendingFolderChange({ next: resolved, games });
        return;
      }
      await persist(resolved);
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  };
  const confirmPendingFolderChange = async (updateGames: boolean) => {
    if (!pendingFolderChange) return;
    const pending = pendingFolderChange;
    await persist(pending.next, updateGames ? pending.games : [], pending.install);
  };
  // Switching importers or pages drops the draft, so leaving with unsaved changes asks first.
  const leave = useBlocker({
    shouldBlockFn: () => dirty || isSaving,
    enableBeforeUnload: false,
    withResolver: true,
  });

  if (!config) return null;

  const hasGameTweaks = !!(config.gimi || config.srmi || config.himi || config.wwmi);
  const xxmiRuntime = config.mode === RuntimeMode.RuntimeXXMI;
  // The 3DMigoto loader injects on its own schedule, so ReShade cannot be loaded ahead of it.
  const reshadeUnsupported = !xxmiRuntime && config.injectionMethod !== "Native";
  const followsSharedLibs = config.xxmiVersion.follow === "shared";
  const libsPin = followsSharedLibs ? sharedLibsVersion : config.xxmiVersion.pinned;
  const selectedCustomDll = selectedDll(config);
  const ownsCustomDll = !!config.customDll || pickingCustomDll;
  const selectedCustomDllName = [importedDll, ...(overview?.customDlls ?? [])].find(
    (dll) => dll?.id === selectedCustomDll,
  )?.name;

  // One choice covers the provider and its version: the shared settings, a provider, or a custom DLL. A config
  // saved while the two followed the shared settings separately is shown by the provider it deploys.
  const libsSource = ownsCustomDll
    ? CUSTOM_DLL_SOURCE
    : config.libsProvider ||
      (followsSharedLibs || overview?.sharedCustomDll || !overview?.sharedLibsProvider
        ? FOLLOW_SHARED
        : overview.sharedLibsProvider);
  const sharedLibsSettings = !overview?.sharedLibsProvider
    ? undefined
    : overview.sharedCustomDll
      ? `${t("page.setting.xxmi.builtin.customDll")} · ${selectedCustomDllName ?? overview.sharedCustomDll}`
      : `${t(`page.setting.xxmi.builtin.libsProviders.${overview.sharedLibsProvider}`)} · ${sharedLibsVersion || t("page.setting.xxmi.builtin.latest")}`;
  // Only a provider has releases to choose from. The legacy runtime keeps the libraries for their injector alone,
  // which the native injector replaces.
  const choosesLibsVersion = xxmiRuntime
    ? libsSource !== FOLLOW_SHARED && libsSource !== CUSTOM_DLL_SOURCE && !launchesCustomDll(config)
    : // A pin left from another injection method is still tracked, so it stays reachable.
      config.injectionMethod !== "Native" || !!config.xxmiVersion.pinned;

  return (
    <main className="flex min-h-0 flex-1 flex-col">
      <Tabs
        inert={isSaving}
        value={tab}
        onValueChange={(value) => setTab(String(value))}
        className="min-h-0 flex-1 gap-0"
      >
        {/* The header stays outside the scroll area; a matching gutter keeps it aligned with the body. */}
        <div className="shrink-0 scrollbar-gutter-stable overflow-hidden border-b">
          <div className="mx-auto w-full max-w-2xl space-y-3 px-4 pt-4 pb-px">
            <div className="flex items-center gap-2">
              <GameIcon gameName={importer} className="size-7 rounded-md" />
              <span className="font-semibold">{importer}</span>
              <div className="ml-auto flex gap-2">
                <Button
                  variant="outline"
                  disabled={dirty || packageNeedsInstall || isSaving}
                  title={dirty ? t("page.setting.xxmi.builtin.unsavedChanges") : undefined}
                  onClickPromise={async () => {
                    try {
                      await startImporter(importer);
                    } catch (error) {
                      toast.error(toErrorMessage(error));
                    }
                  }}
                >
                  <PlayIcon />
                  {t("page.setting.xxmi.builtin.launch")}
                </Button>
                <Button disabled={!dirty || saveBlocked || isSaving} onClickPromise={() => save()}>
                  {t("g.save")}
                </Button>
              </div>
            </div>
            {saveBlocked && (
              <Alert>
                <TriangleAlertIcon />
                <AlertDescription>
                  {t("page.setting.xxmi.builtin.packageInstallRequired")}
                </AlertDescription>
              </Alert>
            )}
            <TabsList variant="line" className="w-full justify-start">
              <TabsTrigger value="general">{t("page.setting.xxmi.builtin.general")}</TabsTrigger>
              <TabsTrigger value="package">{t("page.setting.xxmi.builtin.packageTab")}</TabsTrigger>
              {hasGameTweaks && (
                <TabsTrigger value="game">{t("page.setting.xxmi.builtin.gameTweaks")}</TabsTrigger>
              )}
              <TabsTrigger value="advanced">{t("page.setting.xxmi.builtin.advanced")}</TabsTrigger>
              <TabsTrigger value="tools">{t("page.setting.xxmi.builtin.tools")}</TabsTrigger>
            </TabsList>
          </div>
        </div>

        <div className="relative flex min-h-0 flex-1 flex-col">
          {/* The unsaved notice floats over the body so toggling it never resizes the header or the scroll area. */}
          {dirty && (
            <div className="pointer-events-none absolute inset-x-0 bottom-0 z-10 scrollbar-gutter-stable overflow-hidden">
              <div className="mx-auto w-full max-w-2xl px-4 pt-2 pb-4">
                <div
                  role="status"
                  className="pointer-events-auto flex items-center justify-between gap-3 rounded-md border bg-popover/50 px-3 py-1.5 text-xs text-popover-foreground shadow-md backdrop-blur-lg"
                >
                  <span>{t("page.setting.xxmi.builtin.unsavedChanges")}</span>
                  <Button
                    variant="ghost"
                    size="xs"
                    onClick={() => {
                      setConfig(null);
                      setPickingCustomDll(false);
                    }}
                  >
                    {t("page.setting.xxmi.builtin.discard")}
                  </Button>
                </div>
              </div>
            </div>
          )}
          <div className="min-h-0 flex-1 scrollbar-gutter-stable overflow-y-auto">
            {/* Extra bottom room while dirty keeps the last rows reachable above the floating notice. */}
            <div className={cn("mx-auto w-full max-w-2xl p-4", dirty && "pb-16")}>
              <TabsContent value="general" className="flex flex-col gap-6">
                <Section>
                  <SectionContent>
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.enabled")}
                      checked={config.enabled}
                      onCheckedChange={(enabled) => setConfig({ ...config, enabled })}
                    />
                    <PathField
                      label={t("page.setting.xxmi.builtin.importerFolder")}
                      description={t("page.setting.xxmi.builtin.importerFolderDescription")}
                      value={config.importerFolder}
                      onValueChange={(importerFolder) => setConfig({ ...config, importerFolder })}
                    />
                    <PathField
                      label={t("page.setting.xxmi.builtin.gameFolder")}
                      description={t("page.setting.xxmi.builtin.gameFolderDescription")}
                      value={config.gameFolder}
                      onValueChange={(gameFolder) => setConfig({ ...config, gameFolder })}
                    >
                      <Button
                        variant="outline"
                        onClickPromise={async () => {
                          try {
                            setDetectedFolders(await XXMI.DetectGameFolders(importer));
                          } catch (error) {
                            toast.error(toErrorMessage(error));
                          }
                        }}
                      >
                        <ScanSearchIcon />
                        {t("page.setting.xxmi.builtin.detectGame")}
                      </Button>
                    </PathField>
                    <SectionRow
                      title={t("page.setting.xxmi.builtin.mode")}
                      description={t("page.setting.xxmi.builtin.modeDescription")}
                    >
                      <ButtonGroup>
                        {(
                          [
                            [RuntimeMode.RuntimeXXMI, "XXMI"],
                            [RuntimeMode.RuntimeLegacy, "3DMigoto"],
                          ] as const
                        ).map(([mode, label]) => (
                          <Button
                            key={mode}
                            variant={config.mode === mode ? "default" : "outline"}
                            aria-pressed={config.mode === mode}
                            onClick={() => setConfig({ ...config, mode })}
                          >
                            {label}
                          </Button>
                        ))}
                      </ButtonGroup>
                    </SectionRow>
                    {config.mode === RuntimeMode.RuntimeLegacy && (
                      <div className="space-y-3 rounded-md bg-muted/50 p-3">
                        <SelectRow
                          label={t("page.setting.xxmi.builtin.legacy")}
                          value={config.legacyRuntime || FOLLOW_LATEST}
                          options={[
                            {
                              value: FOLLOW_LATEST,
                              label: t("page.setting.xxmi.builtin.newestCached"),
                            },
                            ...(legacyRuntimes?.map((runtime) => runtime.id) ?? []),
                          ]}
                          onValueChange={(value) =>
                            setConfig({
                              ...config,
                              legacyRuntime: value === FOLLOW_LATEST ? "" : value,
                            })
                          }
                        />
                        <Alert>
                          <TriangleAlertIcon />
                          <AlertDescription>
                            {t("page.setting.xxmi.builtin.legacyWarning")}
                          </AlertDescription>
                        </Alert>
                      </div>
                    )}
                  </SectionContent>
                </Section>

                <Section>
                  <SectionHeader>
                    <SectionTitle>{t("page.setting.xxmi.builtin.launchOptions")}</SectionTitle>
                  </SectionHeader>
                  <SectionContent>
                    <SelectRow
                      label={t("page.setting.xxmi.builtin.gameLaunch")}
                      description={t("page.setting.xxmi.builtin.gameLaunchDescription")}
                      value={config.gameLaunch}
                      options={(["Direct", "Steam", "Epic", "Custom", "Manual"] as const).map(
                        (value) => ({
                          value,
                          label: t(`page.setting.xxmi.builtin.gameLaunch${value}`),
                        }),
                      )}
                      onValueChange={(gameLaunch) => setConfig({ ...config, gameLaunch })}
                    />
                    {config.gameLaunch === "Custom" && (
                      <div className="space-y-3 rounded-md bg-muted/50 p-3">
                        <SectionRow
                          title={t("page.setting.xxmi.builtin.customLaunch")}
                          description={t("page.setting.xxmi.builtin.customLaunchDescription")}
                        />
                        <Input
                          aria-label={t("page.setting.xxmi.builtin.customLaunch")}
                          className="font-mono"
                          spellCheck={false}
                          value={config.customLaunch.command}
                          onChange={(event) =>
                            setConfig({ ...config, customLaunch: { command: event.target.value } })
                          }
                        />
                        <Alert>
                          <ShieldAlertIcon />
                          <AlertDescription>
                            {t("page.setting.xxmi.builtin.elevatedCommandWarning")}
                          </AlertDescription>
                        </Alert>
                      </div>
                    )}
                    {config.gameLaunch === "Steam" && (
                      <div className="space-y-3 rounded-md bg-muted/50 p-3">
                        <ToggleRow
                          label={t("page.setting.xxmi.builtin.configurePlatformLaunchOptions")}
                          description={t(
                            "page.setting.xxmi.builtin.configurePlatformLaunchOptionsDescription",
                          )}
                          checked={config.configurePlatformLaunchOptions}
                          onCheckedChange={(configurePlatformLaunchOptions) =>
                            setConfig({ ...config, configurePlatformLaunchOptions })
                          }
                        />
                        {importer === "ZZMI" && (
                          <ToggleRow
                            label={t("page.setting.xxmi.builtin.skipPlatformGameLauncher")}
                            description={t(
                              "page.setting.xxmi.builtin.skipPlatformGameLauncherDescription",
                            )}
                            checked={config.skipPlatformGameLauncher}
                            disabled={!config.configurePlatformLaunchOptions}
                            onCheckedChange={(skipPlatformGameLauncher) =>
                              setConfig({ ...config, skipPlatformGameLauncher })
                            }
                          />
                        )}
                      </div>
                    )}
                    <SelectRow
                      label={t("page.setting.xxmi.builtin.windowMode")}
                      description={t("page.setting.xxmi.builtin.windowModeDescription")}
                      value={config.windowMode}
                      options={["Windowed", "Borderless", "Fullscreen", "Exclusive Fullscreen"]}
                      onValueChange={(windowMode) => setConfig({ ...config, windowMode })}
                    />
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.useLaunchOptions")}
                      description={
                        config.wwmi
                          ? t("page.setting.xxmi.builtin.launchOptionsDescriptionWWMI")
                          : undefined
                      }
                      checked={config.useLaunchOptions}
                      onCheckedChange={(useLaunchOptions) =>
                        setConfig({ ...config, useLaunchOptions })
                      }
                    >
                      <Input
                        aria-label={t("page.setting.xxmi.builtin.useLaunchOptions")}
                        className="font-mono"
                        spellCheck={false}
                        value={config.launchOptions}
                        onChange={(event) =>
                          setConfig({ ...config, launchOptions: event.target.value })
                        }
                      />
                    </ToggleRow>
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.reshade.title")}
                      description={
                        reshadeUnsupported
                          ? t("page.setting.xxmi.builtin.reshade.legacyUnsupported")
                          : t("page.setting.xxmi.builtin.reshade.toggleDescription")
                      }
                      checked={config.reshade.enabled}
                      disabled={reshadeUnsupported && !config.reshade.enabled}
                      onCheckedChange={(enabled) => setConfig({ ...config, reshade: { enabled } })}
                    >
                      <div className="flex items-center justify-between gap-4">
                        <p className="text-xs text-muted-foreground">
                          {t("page.setting.xxmi.builtin.reshade.managePageDescription")}
                        </p>
                        <Button
                          variant="outline"
                          size="sm"
                          className="shrink-0"
                          onClick={() => void navigate({ to: "/xxmi/reshade" })}
                        >
                          {t("page.setting.xxmi.builtin.reshade.managePage")}
                        </Button>
                      </div>
                    </ToggleRow>
                    <SelectRow
                      label={t("page.setting.xxmi.builtin.startMethod")}
                      description={t("page.setting.xxmi.builtin.startMethodDescription")}
                      value={config.processStartMethod}
                      options={["Native", "Shell"]}
                      onValueChange={(processStartMethod) =>
                        setConfig({ ...config, processStartMethod })
                      }
                    />
                    <SelectRow
                      label={t("page.setting.xxmi.builtin.priority")}
                      value={config.processPriority}
                      options={["Low", "BelowNormal", "Normal", "AboveNormal", "High", "Realtime"]}
                      onValueChange={(processPriority) => setConfig({ ...config, processPriority })}
                    />
                    <SelectRow
                      label={t("page.setting.xxmi.builtin.injectMode")}
                      description={t("page.setting.xxmi.builtin.injectModeDescription")}
                      value={config.xxmiDLLInjectMode}
                      options={["Hook", "Inject", "Bypass"]}
                      onValueChange={(xxmiDLLInjectMode) =>
                        setConfig({ ...config, xxmiDLLInjectMode })
                      }
                    />
                    <SelectRow
                      label={t("page.setting.xxmi.builtin.injectionMethod")}
                      description={t("page.setting.xxmi.builtin.injectionMethodDescription")}
                      value={config.injectionMethod || "Default"}
                      options={[
                        {
                          value: "Default",
                          label: t("page.setting.xxmi.builtin.injectionDefault"),
                        },
                        { value: "Native", label: t("page.setting.xxmi.builtin.injectionNative") },
                      ]}
                      onValueChange={(injectionMethod) => setConfig({ ...config, injectionMethod })}
                    />
                    <NumberRow
                      label={t("page.setting.xxmi.builtin.timeout")}
                      description={t("page.setting.xxmi.builtin.timeoutDescription")}
                      min={0}
                      value={config.processTimeout}
                      onValueChange={(processTimeout) => setConfig({ ...config, processTimeout })}
                    />
                    <NumberRow
                      label={t("page.setting.xxmi.builtin.initDelay")}
                      description={t("page.setting.xxmi.builtin.initDelayDescription")}
                      min={0}
                      value={config.xxmiDLLInitDelay}
                      onValueChange={(xxmiDLLInitDelay) =>
                        setConfig({ ...config, xxmiDLLInitDelay })
                      }
                    />
                  </SectionContent>
                </Section>
              </TabsContent>

              <TabsContent value="package" className="flex flex-col gap-6">
                {(xxmiRuntime || choosesLibsVersion) && (
                  <Section>
                    <SectionHeader>
                      <SectionTitle>{t("page.setting.xxmi.builtin.libs")}</SectionTitle>
                    </SectionHeader>
                    <SectionContent>
                      {xxmiRuntime && (
                        <SelectRow
                          label={t("page.setting.xxmi.builtin.libsProvider")}
                          description={
                            libsSource === FOLLOW_SHARED && sharedLibsSettings
                              ? t("page.setting.xxmi.builtin.libsSharedSettings", {
                                  settings: sharedLibsSettings,
                                })
                              : undefined
                          }
                          value={libsSource}
                          options={[
                            {
                              value: FOLLOW_SHARED,
                              label: t("page.setting.xxmi.builtin.libsFollowSharedSettings"),
                            },
                            ...libsProviders.map((provider) => ({
                              value: provider,
                              label: t(`page.setting.xxmi.builtin.libsProviders.${provider}`),
                            })),
                            {
                              value: CUSTOM_DLL_SOURCE,
                              label: t("page.setting.xxmi.builtin.customDll"),
                            },
                          ]}
                          onValueChange={(value) => {
                            setPickingCustomDll(value === CUSTOM_DLL_SOURCE);
                            if (value === CUSTOM_DLL_SOURCE) return;
                            if (value === FOLLOW_SHARED) {
                              setConfig({
                                ...config,
                                libsProvider: "",
                                customDll: "",
                                xxmiVersion: { follow: "shared" },
                              });
                              return;
                            }
                            const next = { ...config, libsProvider: value, customDll: "" };

                            // A pinned version names a release of the provider it was picked from, so only that
                            // provider keeps the version in effect, the shared one included.
                            const keepsVersion = followsSharedLibs
                              ? value === overview?.sharedLibsProvider
                              : deployedLibsProvider(next) === libsProvider;
                            const version =
                              followsSharedLibs && sharedLibsVersion
                                ? { pinned: sharedLibsVersion, notify: false }
                                : followsSharedLibs
                                  ? { follow: "latest" }
                                  : config.xxmiVersion;
                            setConfig({
                              ...next,
                              xxmiVersion: keepsVersion ? version : { follow: "latest" },
                            });
                          }}
                        />
                      )}
                      {xxmiRuntime && ownsCustomDll && (
                        <div className="rounded-md bg-muted/50 p-3">
                          <CustomDLLField
                            dropTargetId={FileDropTargetID.xxmiImporterCustomDll}
                            selected={
                              config.customDll
                                ? { id: config.customDll, name: selectedCustomDllName }
                                : undefined
                            }
                            onPick={async (path) => {
                              const dll = await XXMI.ImportCustomDLL(path);
                              setImportedDll(dll);
                              if (config.migoto.unsafeMode)
                                setConfig({ ...config, customDll: dll.id });
                              else setPendingCustomDll(dll);
                            }}
                          />
                        </div>
                      )}
                      {selectedCustomDll && !config.migoto.unsafeMode && (
                        <Alert>
                          <TriangleAlertIcon />
                          <AlertDescription className="flex items-center justify-between gap-4">
                            <span>{t("page.setting.xxmi.builtin.customDllInactive")}</span>
                            <Button
                              variant="outline"
                              size="sm"
                              className="shrink-0"
                              onClick={() =>
                                setConfig({
                                  ...config,
                                  migoto: { ...config.migoto, unsafeMode: true },
                                })
                              }
                            >
                              {t("page.setting.xxmi.builtin.customDllEnableUnsafe")}
                            </Button>
                          </AlertDescription>
                        </Alert>
                      )}
                      {customDll && (
                        <Alert>
                          <ShieldAlertIcon />
                          <AlertDescription className="flex items-center justify-between gap-4">
                            <span>{t("page.setting.xxmi.builtin.customDllDescription")}</span>
                            {/* Restoring saves the config server-side, so a stale draft would re-enable unsafe mode. */}
                            <Button
                              variant="outline"
                              size="sm"
                              className="shrink-0"
                              disabled={dirty}
                              title={
                                dirty ? t("page.setting.xxmi.builtin.unsavedChanges") : undefined
                              }
                              onClickPromise={async () => {
                                try {
                                  const warnings = await XXMI.RestoreOfficialDLL(importer);
                                  refresh();
                                  toast.success(t("page.setting.xxmi.builtin.officialDllRestored"));
                                  warnings?.forEach((warning) => toast.warning(warning));
                                } catch (error) {
                                  toast.error(toErrorMessage(error));
                                }
                              }}
                            >
                              {t("page.setting.xxmi.builtin.restoreOfficialDll")}
                            </Button>
                          </AlertDescription>
                        </Alert>
                      )}
                      {choosesLibsVersion && (
                        <SelectRow
                          label={t("page.setting.xxmi.builtin.libsVersion")}
                          description={
                            [
                              !xxmiRuntime && t("page.setting.xxmi.builtin.libsVersionLegacy"),
                              followsSharedLibs &&
                                t("page.setting.xxmi.builtin.libsSharedCurrent", {
                                  version:
                                    sharedLibsVersion || t("page.setting.xxmi.builtin.latest"),
                                }),
                            ]
                              .filter(Boolean)
                              .join(" ") || undefined
                          }
                          value={
                            followsSharedLibs
                              ? FOLLOW_SHARED
                              : config.xxmiVersion.pinned || FOLLOW_LATEST
                          }
                          options={[
                            // The legacy runtime has no provider to follow the shared settings through, and a config
                            // saved with only the version following them keeps that choice until it is changed.
                            ...(!xxmiRuntime || followsSharedLibs
                              ? [
                                  {
                                    value: FOLLOW_SHARED,
                                    label: t("page.setting.xxmi.builtin.libsFollowShared"),
                                  },
                                ]
                              : []),
                            { value: FOLLOW_LATEST, label: t("page.setting.xxmi.builtin.latest") },
                            ...(libsReleases?.map((release) => release.version) ?? []),
                          ]}
                          onValueChange={(value) =>
                            setConfig({
                              ...config,
                              // The shared provider is shown as the importer's own while only its version is, so
                              // choosing a version makes both its own.
                              libsProvider: xxmiRuntime ? libsSource : config.libsProvider,
                              xxmiVersion:
                                value === FOLLOW_SHARED
                                  ? { follow: "shared" }
                                  : value === FOLLOW_LATEST
                                    ? { follow: "latest" }
                                    : {
                                        pinned: value,
                                        // A new pin starts fixed; picking another version keeps the notices the user chose.
                                        notify: config.xxmiVersion.pinned
                                          ? config.xxmiVersion.notify
                                          : false,
                                      },
                            })
                          }
                        />
                      )}
                      {choosesLibsVersion && !followsSharedLibs && config.xxmiVersion.pinned && (
                        <ToggleRow
                          label={t("page.setting.xxmi.builtin.libsNotify")}
                          description={t("page.setting.xxmi.builtin.libsNotifyDescription")}
                          checked={!!config.xxmiVersion.notify}
                          onCheckedChange={(notify) =>
                            setConfig({
                              ...config,
                              xxmiVersion: { pinned: config.xxmiVersion.pinned, notify },
                            })
                          }
                        />
                      )}
                    </SectionContent>
                  </Section>
                )}

                <Section>
                  <SectionHeader>
                    <SectionTitle>{t("page.setting.xxmi.builtin.packageVersion")}</SectionTitle>
                  </SectionHeader>
                  <SectionContent>
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                      <span>
                        {t("page.setting.xxmi.builtin.currentPin")}:{" "}
                        <span className="font-medium text-foreground">
                          {config.packageVersion.pinned || t("page.setting.xxmi.builtin.latest")}
                        </span>
                      </span>
                      <span>
                        {t("page.setting.xxmi.packageVersionCurrent", {
                          version: packageInstalledInFolder
                            ? installedVersion
                            : t("page.setting.xxmi.packageVersionUnknown"),
                        })}
                      </span>
                      {packageVerification && (
                        <span>
                          {t("page.setting.xxmi.builtin.installedVerification", {
                            version: packageVerification.version,
                            method:
                              packageVerification.method === "ecdsa"
                                ? t("page.setting.xxmi.builtin.verificationECDSA")
                                : packageVerification.method === "digest"
                                  ? t("page.setting.xxmi.builtin.verificationDigest")
                                  : t("page.setting.xxmi.builtin.verificationNone"),
                          })}
                        </span>
                      )}
                    </div>
                    <div className="max-h-72 space-y-0.5 overflow-y-auto">
                      <VersionOption
                        selected={!config.packageVersion.pinned}
                        onSelect={() => {
                          setAllowUnsigned(false);
                          setConfig({ ...config, packageVersion: { follow: "latest" } });
                        }}
                      >
                        {t("page.setting.xxmi.builtin.latest")}
                      </VersionOption>
                      {releases?.map((release) => (
                        <VersionOption
                          key={release.version}
                          selected={config.packageVersion.pinned === release.version}
                          onSelect={() => {
                            setAllowUnsigned(false);
                            // An uninstalled version cannot be saved, so it is only pinned once the dialog installs it.
                            if (isInstalledPackageVersion(release.version)) {
                              setConfig({ ...config, packageVersion: { pinned: release.version } });
                            } else {
                              setPackageDialogVersion(release.version);
                            }
                          }}
                          actions={
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`${t("page.setting.xxmi.builtin.packageDetails")} ${release.version}`}
                              title={t("page.setting.xxmi.builtin.packageDetails")}
                              onClick={() => {
                                setAllowUnsigned(false);
                                setPackageDialogVersion(release.version);
                              }}
                            >
                              <FileTextIcon />
                            </Button>
                          }
                        >
                          <span className="font-mono">{release.version}</span>
                          {isInstalledPackageVersion(release.version) && (
                            <span className="text-xs text-muted-foreground">
                              {t("page.setting.xxmi.builtin.packageInstalled")}
                            </span>
                          )}
                          {!release.signed && (
                            <Badge variant="destructive">
                              {t("page.setting.xxmi.builtin.unsigned")}
                            </Badge>
                          )}
                        </VersionOption>
                      ))}
                    </div>
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.overwriteINI")}
                      description={t("page.setting.xxmi.builtin.overwriteINIDescription")}
                      checked={config.overwriteINI}
                      onCheckedChange={(overwriteINI) => setConfig({ ...config, overwriteINI })}
                    />
                  </SectionContent>
                </Section>
              </TabsContent>

              {hasGameTweaks && (
                <TabsContent value="game" className="flex flex-col gap-6">
                  <Section>
                    <SectionContent>
                      {/* Only WWMI has optional game-side settings; GIMI always turns off Dynamic
                          Character Resolution, and SRMI and HIMI adjust nothing. */}
                      {config.wwmi && (
                        <ToggleRow
                          label={t("page.setting.xxmi.builtin.configureGame")}
                          description={t("page.setting.xxmi.builtin.configureGameDescriptionWWMI")}
                          checked={config.configureGame}
                          onCheckedChange={(configureGame) =>
                            setConfig({ ...config, configureGame })
                          }
                        />
                      )}
                      {config.gimi && (
                        <>
                          <ToggleRow
                            label={t("page.setting.xxmi.builtin.unlockFPS")}
                            description={t("page.setting.xxmi.builtin.unlockFPSDescriptionGIMI")}
                            checked={config.gimi.unlockFPS}
                            onCheckedChange={(unlockFPS) =>
                              setConfig({ ...config, gimi: { ...config.gimi!, unlockFPS } })
                            }
                          >
                            <NumberRow
                              label={t("page.setting.xxmi.builtin.fpsTarget")}
                              description={t("page.setting.xxmi.builtin.fpsTargetDescriptionGIMI")}
                              min={30}
                              max={1000}
                              value={config.gimi.unlockFPSValue}
                              onValueChange={(unlockFPSValue) =>
                                setConfig({ ...config, gimi: { ...config.gimi!, unlockFPSValue } })
                              }
                            />
                          </ToggleRow>
                          <ToggleRow
                            label={t("page.setting.xxmi.builtin.enableHDR")}
                            description={t("page.setting.xxmi.builtin.enableHDRDescription")}
                            checked={config.gimi.enableHDR}
                            onCheckedChange={(enableHDR) =>
                              setConfig({ ...config, gimi: { ...config.gimi!, enableHDR } })
                            }
                          />
                        </>
                      )}
                      {config.srmi && (
                        <ToggleRow
                          label={t("page.setting.xxmi.builtin.unlockFPS")}
                          description={t("page.setting.xxmi.builtin.unlockFPSDescriptionSRMI")}
                          checked={config.srmi.unlockFPS}
                          onCheckedChange={(unlockFPS) =>
                            setConfig({ ...config, srmi: { ...config.srmi!, unlockFPS } })
                          }
                        />
                      )}
                      {config.himi && (
                        <ToggleRow
                          label={t("page.setting.xxmi.builtin.unlockFPS")}
                          description={t("page.setting.xxmi.builtin.unlockFPSDescriptionHIMI")}
                          checked={config.himi.unlockFPS}
                          onCheckedChange={(unlockFPS) =>
                            setConfig({ ...config, himi: { ...config.himi!, unlockFPS } })
                          }
                        >
                          <NumberRow
                            label={t("page.setting.xxmi.builtin.fpsTarget")}
                            min={30}
                            max={1000}
                            value={config.himi.unlockFPSValue}
                            onValueChange={(unlockFPSValue) =>
                              setConfig({ ...config, himi: { ...config.himi!, unlockFPSValue } })
                            }
                          />
                        </ToggleRow>
                      )}
                      {config.wwmi &&
                        (["unlockFPS", "forceMaxLODBias", "disableWoundedFX"] as const).map(
                          (field) => (
                            <ToggleRow
                              key={field}
                              label={t(`page.setting.xxmi.builtin.${field}`)}
                              description={t(
                                field === "unlockFPS"
                                  ? "page.setting.xxmi.builtin.unlockFPSDescriptionWWMI"
                                  : `page.setting.xxmi.builtin.${field}Description`,
                              )}
                              checked={config.wwmi![field]}
                              onCheckedChange={(value) =>
                                setConfig({
                                  ...config,
                                  woundedFXDecided:
                                    field === "disableWoundedFX" ? true : config.woundedFXDecided,
                                  wwmi: { ...config.wwmi!, [field]: value },
                                })
                              }
                            />
                          ),
                        )}
                    </SectionContent>
                  </Section>
                  {config.wwmi && (
                    <Section>
                      <SectionContent>
                        <WWMIGraphicsSettings
                          options={config.wwmi}
                          onChange={(wwmi) => setConfig({ ...config, wwmi })}
                        />
                      </SectionContent>
                    </Section>
                  )}
                </TabsContent>
              )}

              <TabsContent value="advanced" className="flex flex-col gap-6">
                <Section>
                  <SectionHeader>
                    <SectionTitle>{t("page.setting.xxmi.builtin.commands")}</SectionTitle>
                  </SectionHeader>
                  <SectionContent>
                    {(["runPreLaunch", "runPostLoad"] as const).map((field) => (
                      <ToggleRow
                        key={field}
                        label={t(
                          field === "runPreLaunch"
                            ? "page.setting.xxmi.builtin.preLaunch"
                            : "page.setting.xxmi.builtin.postLoad",
                        )}
                        description={t(
                          field === "runPreLaunch"
                            ? "page.setting.xxmi.builtin.preLaunchDescription"
                            : "page.setting.xxmi.builtin.postLoadDescription",
                        )}
                        checked={config[field].enabled}
                        onCheckedChange={(enabled) =>
                          setConfig({ ...config, [field]: { ...config[field], enabled } })
                        }
                      >
                        <Input
                          aria-label={t(
                            field === "runPreLaunch"
                              ? "page.setting.xxmi.builtin.preLaunch"
                              : "page.setting.xxmi.builtin.postLoad",
                          )}
                          className="font-mono"
                          spellCheck={false}
                          value={config[field].command}
                          onChange={(event) =>
                            setConfig({
                              ...config,
                              [field]: { ...config[field], command: event.target.value },
                            })
                          }
                        />
                        <ToggleRow
                          label={t("page.setting.xxmi.builtin.waitForCommand")}
                          checked={config[field].wait}
                          onCheckedChange={(wait) =>
                            setConfig({ ...config, [field]: { ...config[field], wait } })
                          }
                        />
                      </ToggleRow>
                    ))}
                    {config.gameLaunch !== "Direct" && (
                      <div className="space-y-1.5">
                        <SectionRow
                          title={t("page.setting.xxmi.builtin.gameProcessExe")}
                          description={t("page.setting.xxmi.builtin.gameProcessExeDescription")}
                        />
                        <Input
                          aria-label={t("page.setting.xxmi.builtin.gameProcessExe")}
                          className="font-mono"
                          spellCheck={false}
                          value={config.gameProcessExe}
                          onChange={(event) =>
                            setConfig({ ...config, gameProcessExe: event.target.value.trim() })
                          }
                        />
                      </div>
                    )}
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.extraLibraries")}
                      description={
                        config.mode === RuntimeMode.RuntimeLegacy &&
                        config.injectionMethod !== "Native" &&
                        !cachedLibs?.length
                          ? `${t("page.setting.xxmi.builtin.libs")}: ${t("page.setting.xxmi.builtin.notInstalled")}`
                          : t("page.setting.xxmi.builtin.extraLibrariesDescription")
                      }
                      checked={config.extraLibraries.enabled}
                      disabled={
                        config.mode === RuntimeMode.RuntimeLegacy &&
                        config.injectionMethod !== "Native" &&
                        !cachedLibs?.some((entry) => !libsPin || entry.version === libsPin)
                      }
                      onCheckedChange={(enabled) =>
                        setConfig({
                          ...config,
                          extraLibraries: { ...config.extraLibraries, enabled },
                        })
                      }
                    >
                      <Input
                        aria-label={t("page.setting.xxmi.builtin.extraLibraries")}
                        className="font-mono"
                        spellCheck={false}
                        value={config.extraLibraries.paths?.join(";") ?? ""}
                        onChange={(event) =>
                          setConfig({
                            ...config,
                            extraLibraries: {
                              ...config.extraLibraries,
                              paths: event.target.value
                                .split(";")
                                .map((path) => path.trim())
                                .filter(Boolean),
                            },
                          })
                        }
                      />
                    </ToggleRow>
                  </SectionContent>
                </Section>

                <Section>
                  <SectionHeader>
                    <SectionTitle>3DMigoto</SectionTitle>
                  </SectionHeader>
                  <SectionContent>
                    {(
                      [
                        "enforceRendering",
                        "enableHunting",
                        "dumpShaders",
                        "muteWarnings",
                        "clearUnknownSettings",
                      ] as const
                    ).map((field) => (
                      <ToggleRow
                        key={field}
                        label={t(`page.setting.xxmi.builtin.${field}`)}
                        description={t(`page.setting.xxmi.builtin.${field}Description`)}
                        checked={config.migoto[field]}
                        onCheckedChange={(value) =>
                          setConfig({ ...config, migoto: { ...config.migoto, [field]: value } })
                        }
                      />
                    ))}
                    <SelectRow
                      label={t("page.setting.xxmi.builtin.logLevel")}
                      description={t("page.setting.xxmi.builtin.logLevelDescription")}
                      value={config.migoto.logLevel}
                      options={(["Disabled", "Warning", "Info", "Debug"] as const).map((value) => ({
                        value,
                        label: t(`page.setting.xxmi.builtin.logLevel${value}`),
                      }))}
                      onValueChange={(logLevel) =>
                        setConfig({ ...config, migoto: { ...config.migoto, logLevel } })
                      }
                    />
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.input")}
                      description={t("page.setting.xxmi.builtin.inputDescription")}
                      checked={config.migoto.input}
                      onCheckedChange={(input) =>
                        setConfig({ ...config, migoto: { ...config.migoto, input } })
                      }
                    />
                    <SelectRow
                      label={t("page.setting.xxmi.builtin.inputDisableMode")}
                      description={t("page.setting.xxmi.builtin.inputDisableModeDescription")}
                      value={config.migoto.inputDisableMode}
                      options={(["Mods", "All"] as const).map((value) => ({
                        value,
                        label: t(`page.setting.xxmi.builtin.inputDisableMode${value}`),
                      }))}
                      onValueChange={(inputDisableMode) =>
                        setConfig({ ...config, migoto: { ...config.migoto, inputDisableMode } })
                      }
                    />
                    <div className="space-y-1.5">
                      <SectionRow
                        title={t("page.setting.xxmi.builtin.toggleInput")}
                        description={t("page.setting.xxmi.builtin.toggleInputDescription")}
                      />
                      <Input
                        aria-label={t("page.setting.xxmi.builtin.toggleInput")}
                        className="font-mono"
                        spellCheck={false}
                        value={config.migoto.toggleInput}
                        onChange={(event) =>
                          setConfig({
                            ...config,
                            migoto: { ...config.migoto, toggleInput: event.target.value },
                          })
                        }
                      />
                    </div>
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.unsafeMode")}
                      description={
                        config.migoto.unsafeMode
                          ? `${t("page.setting.xxmi.builtin.unsafeModeDescription")} ${t("page.setting.xxmi.builtin.unsafeWarning")}`
                          : t("page.setting.xxmi.builtin.unsafeModeDescription")
                      }
                      checked={config.migoto.unsafeMode}
                      onCheckedChange={(unsafeMode) =>
                        setConfig({ ...config, migoto: { ...config.migoto, unsafeMode } })
                      }
                    />
                  </SectionContent>
                </Section>
              </TabsContent>

              <TabsContent value="tools" className="flex flex-col gap-6">
                <Section>
                  <SectionHeader>
                    <SectionTitle>{t("page.setting.xxmi.builtin.iniOptimizer")}</SectionTitle>
                  </SectionHeader>
                  <SectionContent>
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.optimizeAtLaunch")}
                      description={t("page.setting.xxmi.builtin.optimizeAtLaunchDescription")}
                      checked={config.iniOptimizer.enabled}
                      onCheckedChange={(enabled) =>
                        setConfig({ ...config, iniOptimizer: { ...config.iniOptimizer, enabled } })
                      }
                    />
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.resetOptimizerCache")}
                      description={t("page.setting.xxmi.builtin.resetOptimizerCacheDescription")}
                      checked={config.iniOptimizer.resetCache}
                      onCheckedChange={(resetCache) =>
                        setConfig({
                          ...config,
                          iniOptimizer: { ...config.iniOptimizer, resetCache },
                        })
                      }
                    />
                    <div className="flex gap-2">
                      <Button
                        variant="outline"
                        onClickPromise={async () => {
                          try {
                            setOptimizationPreview(
                              await XXMI.OptimizeMods({
                                importer,
                                dryRun: true,
                                resetCache: config.iniOptimizer.resetCache,
                              }),
                            );
                          } catch (error) {
                            toast.error(toErrorMessage(error));
                          }
                        }}
                      >
                        {t("page.setting.xxmi.builtin.previewOptimization")}
                      </Button>
                      <Button
                        disabled={!optimizationPreview?.changes?.length}
                        onClickPromise={async () => {
                          try {
                            const report = await XXMI.OptimizeMods({
                              importer,
                              dryRun: false,
                              resetCache: config.iniOptimizer.resetCache,
                            });
                            setOptimizationPreview(undefined);
                            toast.success(
                              t("page.setting.xxmi.builtin.optimized", {
                                count: report?.changes?.length ?? 0,
                              }),
                            );
                          } catch (error) {
                            toast.error(toErrorMessage(error));
                          }
                        }}
                      >
                        {t("page.setting.xxmi.builtin.applyOptimization")}
                      </Button>
                    </div>
                    {optimizationPreview && (
                      <div className="max-h-64 space-y-1 overflow-y-auto rounded-md bg-muted/50 p-2 font-mono text-xs">
                        {optimizationPreview.changes?.length ? (
                          optimizationPreview.changes.map((change, index) => (
                            <p key={`${change.path}:${change.line}:${index}`} className="break-all">
                              {change.action}: {change.path}
                              {change.line ? `:${change.line}` : ""} · {change.reason}
                            </p>
                          ))
                        ) : (
                          <p>{t("page.setting.xxmi.builtin.noOptimizationChanges")}</p>
                        )}
                      </div>
                    )}
                  </SectionContent>
                </Section>

                <Section>
                  <SectionHeader>
                    <SectionTitle>{t("page.setting.xxmi.builtin.maintenance")}</SectionTitle>
                  </SectionHeader>
                  <SectionContent>
                    <div className="flex flex-wrap gap-2">
                      <Button
                        variant="outline"
                        title={t("page.setting.xxmi.builtin.repairRuntimeDescription")}
                        onClickPromise={async () => {
                          try {
                            const warnings = await XXMI.RepairRuntime(importer);
                            toast.success(t("page.setting.xxmi.builtin.runtimeRepaired"));
                            warnings?.forEach((warning) => toast.warning(warning));
                          } catch (error) {
                            toast.error(toErrorMessage(error));
                          }
                        }}
                      >
                        {t("page.setting.xxmi.builtin.repairRuntime")}
                      </Button>
                      <Button
                        variant="outline"
                        onClickPromise={async () => {
                          try {
                            await XXMI.OpenImporterFolder(importer);
                          } catch (error) {
                            toast.error(toErrorMessage(error));
                          }
                        }}
                      >
                        {t("page.setting.xxmi.builtin.openImporterFolder")}
                      </Button>
                      <Button
                        variant="outline"
                        title={t("page.setting.xxmi.builtin.createShortcutDescription")}
                        onClickPromise={async () => {
                          try {
                            await XXMI.CreateShortcut(importer);
                            toast.success(t("page.setting.xxmi.builtin.shortcutCreated"));
                          } catch (error) {
                            toast.error(toErrorMessage(error));
                          }
                        }}
                      >
                        {t("page.setting.xxmi.builtin.createShortcut")}
                      </Button>
                    </div>
                  </SectionContent>
                </Section>
              </TabsContent>
            </div>
          </div>
        </div>
      </Tabs>

      <Dialog
        open={packageDialogVersion !== null}
        onOpenChange={(open) => {
          if (open || isSaving) return;
          setPackageDialogVersion(null);
          setAllowUnsigned(false);
        }}
      >
        <DialogContent className="max-h-[80vh] sm:max-w-lg" showCloseButton={!isSaving}>
          <DialogHeader>
            <DialogTitle>
              {importer} · {packageDialogVersion}
            </DialogTitle>
            <DialogDescription>
              {dialogRelease && !isInstalledPackageVersion(dialogRelease.version)
                ? t("page.setting.xxmi.builtin.packageNotInstalled")
                : t("page.setting.xxmi.builtin.packageDetails")}
            </DialogDescription>
          </DialogHeader>
          {dialogRelease && (
            <>
              <div className="flex items-center gap-3 text-xs text-muted-foreground">
                {isInstalledPackageVersion(dialogRelease.version) && (
                  <span>{t("page.setting.xxmi.builtin.packageInstalled")}</span>
                )}
                {dialogRelease.signed ? (
                  <span>{t("page.setting.xxmi.builtin.signed")}</span>
                ) : (
                  <Badge variant="destructive">{t("page.setting.xxmi.builtin.unsigned")}</Badge>
                )}
              </div>
              <p className="max-h-80 overflow-y-auto text-sm whitespace-pre-wrap text-muted-foreground">
                {dialogRelease.notes || t("page.setting.xxmi.builtin.noPackageNotes")}
              </p>
              {!isInstalledPackageVersion(dialogRelease.version) && !dialogRelease.signed && (
                <label className="flex items-center gap-2 text-destructive">
                  <ShieldAlertIcon className="size-4 shrink-0" />
                  <span className="flex-1">{t("page.setting.xxmi.builtin.allowUnsigned")}</span>
                  <Switch
                    checked={allowUnsigned}
                    onCheckedChange={setAllowUnsigned}
                    disabled={isSaving}
                  />
                </label>
              )}
            </>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              disabled={isSaving}
              onClick={() => {
                setPackageDialogVersion(null);
                setAllowUnsigned(false);
              }}
            >
              {t("g.cancel")}
            </Button>
            {dialogRelease && !isInstalledPackageVersion(dialogRelease.version) && (
              <Button
                disabled={isSaving || (!dialogRelease.signed && !allowUnsigned)}
                onClickPromise={async () => {
                  if (isSaving || (!dialogRelease.signed && !allowUnsigned)) return;
                  const next = {
                    ...config,
                    packageVersion: { pinned: dialogRelease.version },
                  };
                  const install = {
                    version: dialogRelease.version,
                    allowUnsigned: !dialogRelease.signed && allowUnsigned,
                  };
                  setConfig(next);
                  setIsSaving(true);
                  try {
                    const resolved = await resolveImporterGameFolder(importer, next);
                    const games = await linkedGamesForImporterMove(
                      saved?.importerFolder,
                      next.importerFolder,
                    );
                    if (games.length) {
                      setPackageDialogVersion(null);
                      setPendingFolderChange({ next: resolved, games, install });
                      return;
                    }
                    await persist(resolved, [], install);
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  } finally {
                    setIsSaving(false);
                  }
                }}
              >
                {t("page.setting.xxmi.builtin.install")}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={detectedFolders !== undefined}
        onOpenChange={(open) => !open && setDetectedFolders(undefined)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("page.setting.xxmi.builtin.detectGame")}</DialogTitle>
          </DialogHeader>
          {detectedFolders?.length ? (
            <div className="max-h-80 space-y-2 overflow-y-auto">
              {detectedFolders.map((candidate) => (
                <Button
                  key={candidate.exePath}
                  variant="outline"
                  className="h-auto w-full justify-start text-left break-all whitespace-normal"
                  onClick={() => {
                    setConfig({ ...config, gameFolder: candidate.path });
                    setDetectedFolders(undefined);
                  }}
                >
                  {candidate.path}
                </Button>
              ))}
            </div>
          ) : (
            <p className="text-muted-foreground">{t("page.setting.xxmi.builtin.noGameFolders")}</p>
          )}
        </DialogContent>
      </Dialog>

      <Dialog
        open={pendingFolderChange !== null}
        onOpenChange={(open) => !open && setPendingFolderChange(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("page.setting.xxmi.builtin.updateModPaths")}</DialogTitle>
          </DialogHeader>
          <p className="text-sm">
            {t("page.setting.xxmi.builtin.updateModPathsDescription", {
              count: pendingFolderChange?.games.length ?? 0,
            })}
          </p>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClickPromise={() => confirmPendingFolderChange(false)}>
              {t("page.setting.xxmi.builtin.keepModPaths")}
            </Button>
            <Button onClickPromise={() => confirmPendingFolderChange(true)}>
              {t("page.setting.xxmi.builtin.updateModPaths")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={leave.status === "blocked"}
        onOpenChange={(open) => !open && leave.reset?.()}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.setting.xxmi.builtin.leaveTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.setting.xxmi.builtin.leaveDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => leave.proceed?.()}>
              {t("page.setting.xxmi.builtin.leaveConfirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog
        open={pendingCustomDll !== null}
        onOpenChange={(open) => !open && setPendingCustomDll(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("page.setting.xxmi.builtin.customDllEnableUnsafeTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.setting.xxmi.builtin.customDllEnableUnsafeDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (!pendingCustomDll) return;
                setConfig({
                  ...config,
                  customDll: pendingCustomDll.id,
                  migoto: { ...config.migoto, unsafeMode: true },
                });
                setPendingCustomDll(null);
              }}
            >
              {t("page.setting.xxmi.builtin.customDllEnableUnsafeConfirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {launchGuardDialog}
    </main>
  );
}

function VersionOption({
  selected,
  onSelect,
  children,
  actions,
}: {
  selected: boolean;
  onSelect: () => void;
  children: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className={cn("flex items-center gap-2 rounded-md text-sm", selected && "bg-muted")}>
      <button
        type="button"
        aria-pressed={selected}
        onClick={onSelect}
        className="flex min-w-0 flex-1 flex-wrap items-center gap-2 rounded-md px-3 py-2 text-left transition-colors hover:bg-muted/60"
      >
        <CheckIcon className={cn("size-4 shrink-0", !selected && "invisible")} />
        {children}
      </button>
      {actions && <div className="flex shrink-0 items-center gap-1 pr-2">{actions}</div>}
    </div>
  );
}
