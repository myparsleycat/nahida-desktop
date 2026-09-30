import { Mod } from "@bindings/mod";
import type { GameConfig } from "@bindings/mod/models";
import { XXMI } from "@bindings/xxmi";
import { RuntimeMode, type ImporterConfig } from "@bindings/xxmi/models";
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
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Separator } from "@renderer/components/ui/separator";
import { Switch } from "@renderer/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@renderer/components/ui/tabs";
import { WWMIGraphicsSettings } from "@renderer/components/xxmi/wwmi-graphics-settings";
import {
  FieldLabel,
  NumberRow,
  PathField,
  SelectRow,
  ToggleRow,
} from "@renderer/components/xxmi/xxmi-fields";
import { useLaunchGuard } from "@renderer/hooks/use-launch-guard";
import { cn } from "@renderer/lib/utils";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { isEqual } from "es-toolkit";
import {
  ArrowLeftIcon,
  CheckIcon,
  PlayIcon,
  ScanSearchIcon,
  ShieldAlertIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// Base UI selects cannot represent an empty string as a regular option value.
const FOLLOW_LATEST = "__latest__";

type PendingImporterFolderChange = {
  next: ImporterConfig;
  games: GameConfig[];
  install?: { version: string; allowUnsigned: boolean };
};

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
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { startImporter, launchGuardDialog } = useLaunchGuard();
  const { data: saved } = useQuery({
    queryKey: ["xxmi:config", importer],
    queryFn: () => XXMI.GetImporterConfig(importer),
  });
  const { data: releases } = useQuery({
    queryKey: ["xxmi:releases", importer],
    queryFn: () => XXMI.ListReleases(`importer:${importer}`),
  });
  const { data: packageVerification } = useQuery({
    queryKey: ["xxmi:package-verification", importer],
    queryFn: () => XXMI.GetImporterPackageVerification(importer),
    enabled: !!saved,
  });
  const { data: libsReleases } = useQuery({
    queryKey: ["xxmi:libs-releases"],
    queryFn: () => XXMI.ListReleases("xxmi-libs"),
  });
  const { data: cachedLibs } = useQuery({
    queryKey: ["xxmi:libs-cache"],
    queryFn: XXMI.ListCachedLibs,
  });
  const { data: overview } = useQuery({ queryKey: ["xxmi:overview"], queryFn: XXMI.GetOverview });
  const customDll = !!overview?.importers?.find((entry) => entry.key === importer)?.customDll;
  const [draft, setConfig] = useState<ImporterConfig | null>(null);
  const config = draft ?? saved ?? null;
  const dirty = draft !== null && !isEqual(draft, saved);
  const { data: legacyRuntimes } = useQuery({
    queryKey: ["xxmi:legacy-cache"],
    queryFn: XXMI.GetLegacyRuntimes,
    enabled: config?.mode === RuntimeMode.RuntimeLegacy,
  });
  const [tab, setTab] = useState("general");
  const [leaveOpen, setLeaveOpen] = useState(false);
  const [selectedPackage, setSelectedPackage] = useState("");
  const selectedRelease = releases?.find((release) => release.version === selectedPackage);
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
  const persist = async (next: ImporterConfig, games: GameConfig[] = []) => {
    try {
      if (next.xxmiVersion.pinned !== saved?.xxmiVersion.pinned) {
        await XXMI.SetImporterVersions(importer, { xxmi: next.xxmiVersion });
      }
      await XXMI.SaveImporterConfig(importer, next);
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
      setConfig(null);
      refresh();
      toast.success(t("page.setting.xxmi.builtin.saved"));
      return true;
    } catch (error) {
      toast.error(toErrorMessage(error));
      return false;
    }
  };
  const finishInstall = async (version: string, allowUnsigned: boolean) => {
    await XXMI.InstallImporterPackage({ importer, version, allowUnsigned });
    refresh();
    toast.success(t("page.setting.xxmi.builtin.installed"));
  };
  const save = async (next = config) => {
    if (!next) return;
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
    const savedOk = await persist(pending.next, updateGames ? pending.games : []);
    if (!savedOk || !pending.install) return;
    try {
      await finishInstall(pending.install.version, pending.install.allowUnsigned);
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  };
  const back = () => navigate({ to: "/xxmi" });

  if (!config) return null;

  const hasGameTweaks = !!(config.gimi || config.srmi || config.himi || config.wwmi);

  return (
    <main className="mx-auto flex w-full flex-1 flex-col p-4 select-none">
      <Tabs value={tab} onValueChange={(value) => setTab(String(value))} className="gap-4">
        <div className="sticky top-0 z-10 -mx-4 -mt-4 space-y-3 border-b bg-background/95 px-4 pt-4 pb-3 backdrop-blur">
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("page.setting.xxmi.builtin.back")}
              title={t("page.setting.xxmi.builtin.back")}
              onClick={() => (dirty ? setLeaveOpen(true) : back())}
            >
              <ArrowLeftIcon />
            </Button>
            <GameIcon gameName={importer} className="size-7 rounded-md" />
            <span className="font-semibold">{importer}</span>
            <div className="ml-auto flex gap-2">
              <Button
                variant="outline"
                disabled={dirty}
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
              <Button disabled={!dirty} onClickPromise={() => save()}>
                {t("g.save")}
              </Button>
            </div>
          </div>
          {dirty && (
            <div
              role="status"
              className="flex items-center justify-between gap-3 rounded-md bg-primary/10 px-3 py-1.5 text-xs"
            >
              <span>{t("page.setting.xxmi.builtin.unsavedChanges")}</span>
              <Button variant="ghost" size="xs" onClick={() => setConfig(null)}>
                {t("page.setting.xxmi.builtin.discard")}
              </Button>
            </div>
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

        <TabsContent value="general" className="space-y-6">
          <Card>
            <CardContent className="space-y-4 text-sm">
              <ToggleRow
                label={t("page.setting.xxmi.builtin.enabled")}
                checked={config.enabled}
                onCheckedChange={(enabled) => setConfig({ ...config, enabled })}
              />
              <Separator />
              <PathField
                label={t("page.setting.xxmi.builtin.importerFolder")}
                value={config.importerFolder}
                onValueChange={(importerFolder) => setConfig({ ...config, importerFolder })}
              />
              <PathField
                label={t("page.setting.xxmi.builtin.gameFolder")}
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
              <Separator />
              <div className="flex items-center justify-between gap-4">
                <FieldLabel label={t("page.setting.xxmi.builtin.mode")} />
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
              </div>
              {config.mode === RuntimeMode.RuntimeLegacy && (
                <div className="space-y-3 border-l-2 pl-4">
                  <SelectRow
                    label={t("page.setting.xxmi.builtin.legacy")}
                    value={config.legacyRuntime || FOLLOW_LATEST}
                    options={[
                      { value: FOLLOW_LATEST, label: t("page.setting.xxmi.builtin.newestCached") },
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
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t("page.setting.xxmi.builtin.launchOptions")}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <SelectRow
                label={t("page.setting.xxmi.builtin.windowMode")}
                value={config.windowMode}
                options={["Windowed", "Borderless", "Fullscreen", "Exclusive Fullscreen"]}
                onValueChange={(windowMode) => setConfig({ ...config, windowMode })}
              />
              <ToggleRow
                label={t("page.setting.xxmi.builtin.useLaunchOptions")}
                checked={config.useLaunchOptions}
                onCheckedChange={(useLaunchOptions) => setConfig({ ...config, useLaunchOptions })}
              >
                <Input
                  aria-label={t("page.setting.xxmi.builtin.useLaunchOptions")}
                  className="font-mono"
                  spellCheck={false}
                  value={config.launchOptions}
                  onChange={(event) => setConfig({ ...config, launchOptions: event.target.value })}
                />
              </ToggleRow>
              <Separator />
              <SelectRow
                label={t("page.setting.xxmi.builtin.startMethod")}
                value={config.processStartMethod}
                options={["Native", "Shell", "Manual"]}
                onValueChange={(processStartMethod) => setConfig({ ...config, processStartMethod })}
              />
              <SelectRow
                label={t("page.setting.xxmi.builtin.priority")}
                value={config.processPriority}
                options={["Low", "BelowNormal", "Normal", "AboveNormal", "High", "Realtime"]}
                onValueChange={(processPriority) => setConfig({ ...config, processPriority })}
              />
              <NumberRow
                label={t("page.setting.xxmi.builtin.timeout")}
                min={0}
                value={config.processTimeout}
                onValueChange={(processTimeout) => setConfig({ ...config, processTimeout })}
              />
              <NumberRow
                label={t("page.setting.xxmi.builtin.initDelay")}
                min={0}
                value={config.xxmiDLLInitDelay}
                onValueChange={(xxmiDLLInitDelay) => setConfig({ ...config, xxmiDLLInitDelay })}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="package" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>{t("page.setting.xxmi.builtin.packageVersion")}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="secondary">
                  {t("page.setting.xxmi.builtin.currentPin")}:{" "}
                  {config.packageVersion.pinned || t("page.setting.xxmi.builtin.latest")}
                </Badge>
                {packageVerification && (
                  <span className="text-xs text-muted-foreground">
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
              <div className="max-h-72 divide-y overflow-y-auto rounded-lg border">
                <VersionOption
                  selected={!config.packageVersion.pinned}
                  onSelect={() => {
                    setSelectedPackage("");
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
                      setSelectedPackage(release.version);
                      setAllowUnsigned(false);
                      setConfig({ ...config, packageVersion: { pinned: release.version } });
                    }}
                  >
                    <span className="font-mono">{release.version}</span>
                    <Badge variant={release.signed ? "secondary" : "destructive"}>
                      {release.signed
                        ? t("page.setting.xxmi.builtin.signed")
                        : t("page.setting.xxmi.builtin.unsigned")}
                    </Badge>
                  </VersionOption>
                ))}
              </div>
              {selectedPackage && (
                <div className="space-y-3 rounded-lg border p-3">
                  {selectedRelease?.notes && (
                    <p className="max-h-48 overflow-y-auto text-xs whitespace-pre-wrap text-muted-foreground">
                      {selectedRelease.notes}
                    </p>
                  )}
                  {selectedRelease && !selectedRelease.signed && (
                    <label className="flex items-center gap-2 text-destructive">
                      <ShieldAlertIcon className="size-4 shrink-0" />
                      <span className="flex-1">{t("page.setting.xxmi.builtin.allowUnsigned")}</span>
                      <Switch checked={allowUnsigned} onCheckedChange={setAllowUnsigned} />
                    </label>
                  )}
                  <Button
                    className="w-full"
                    disabled={!selectedRelease || (!selectedRelease.signed && !allowUnsigned)}
                    onClickPromise={async () => {
                      if (!selectedRelease || (!selectedRelease.signed && !allowUnsigned)) return;
                      const allowPackage = !selectedRelease.signed && allowUnsigned;
                      try {
                        const resolved = await resolveImporterGameFolder(importer, config);
                        const games = await linkedGamesForImporterMove(
                          saved?.importerFolder,
                          config.importerFolder,
                        );
                        if (games.length) {
                          setPendingFolderChange({
                            next: resolved,
                            games,
                            install: { version: selectedPackage, allowUnsigned: allowPackage },
                          });
                          return;
                        }
                        await XXMI.SaveImporterConfig(importer, resolved);
                        await finishInstall(selectedPackage, allowPackage);
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    {t("page.setting.xxmi.builtin.install")}
                  </Button>
                </div>
              )}
              <ToggleRow
                label={t("page.setting.xxmi.builtin.overwriteINI")}
                checked={config.overwriteINI}
                onCheckedChange={(overwriteINI) => setConfig({ ...config, overwriteINI })}
              />
            </CardContent>
          </Card>

          <Card>
            <CardContent className="space-y-4 text-sm">
              <SelectRow
                label={
                  <span className="flex items-center gap-1.5">
                    {t("page.setting.xxmi.builtin.libs")}
                    {customDll && (
                      <Badge variant="outline">{t("page.setting.xxmi.builtin.customDll")}</Badge>
                    )}
                  </span>
                }
                value={config.xxmiVersion.pinned || FOLLOW_LATEST}
                options={[
                  { value: FOLLOW_LATEST, label: t("page.setting.xxmi.builtin.latest") },
                  ...(libsReleases?.map((release) => release.version) ?? []),
                ]}
                onValueChange={(value) =>
                  setConfig({
                    ...config,
                    xxmiVersion: value === FOLLOW_LATEST ? { follow: "latest" } : { pinned: value },
                  })
                }
              />
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
                      title={dirty ? t("page.setting.xxmi.builtin.unsavedChanges") : undefined}
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
            </CardContent>
          </Card>
        </TabsContent>

        {hasGameTweaks && (
          <TabsContent value="game" className="space-y-6">
            <Card>
              <CardContent className="space-y-4 text-sm">
                <ToggleRow
                  label={t("page.setting.xxmi.builtin.configureGame")}
                  checked={config.configureGame}
                  onCheckedChange={(configureGame) => setConfig({ ...config, configureGame })}
                />
                <Separator />
                {config.gimi && (
                  <>
                    <ToggleRow
                      label={t("page.setting.xxmi.builtin.unlockFPS")}
                      checked={config.gimi.unlockFPS}
                      onCheckedChange={(unlockFPS) =>
                        setConfig({ ...config, gimi: { ...config.gimi!, unlockFPS } })
                      }
                    >
                      <NumberRow
                        label={t("page.setting.xxmi.builtin.fpsTarget")}
                        min={30}
                        max={1000}
                        value={config.gimi.unlockFPSValue}
                        onValueChange={(unlockFPSValue) =>
                          setConfig({ ...config, gimi: { ...config.gimi!, unlockFPSValue } })
                        }
                      />
                    </ToggleRow>
                    {(["enableHDR", "disableDCR"] as const).map((field) => (
                      <ToggleRow
                        key={field}
                        label={t(`page.setting.xxmi.builtin.${field}`)}
                        checked={config.gimi![field]}
                        onCheckedChange={(value) =>
                          setConfig({ ...config, gimi: { ...config.gimi!, [field]: value } })
                        }
                      />
                    ))}
                  </>
                )}
                {config.srmi && (
                  <ToggleRow
                    label={t("page.setting.xxmi.builtin.unlockFPS")}
                    checked={config.srmi.unlockFPS}
                    onCheckedChange={(unlockFPS) =>
                      setConfig({ ...config, srmi: { ...config.srmi!, unlockFPS } })
                    }
                  />
                )}
                {config.himi && (
                  <ToggleRow
                    label={t("page.setting.xxmi.builtin.unlockFPS")}
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
                  (
                    ["unlockFPS", "forceMaxLODBias", "applyPerfTweaks", "disableWoundedFX"] as const
                  ).map((field) => (
                    <ToggleRow
                      key={field}
                      label={t(`page.setting.xxmi.builtin.${field}`)}
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
                  ))}
              </CardContent>
            </Card>
            {config.wwmi && (
              <Card>
                <CardContent className="space-y-4 text-sm">
                  <WWMIGraphicsSettings
                    options={config.wwmi}
                    onChange={(wwmi) => setConfig({ ...config, wwmi })}
                  />
                </CardContent>
              </Card>
            )}
          </TabsContent>
        )}

        <TabsContent value="advanced" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>{t("page.setting.xxmi.builtin.commands")}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              {(["runPreLaunch", "runPostLoad"] as const).map((field) => (
                <ToggleRow
                  key={field}
                  label={t(
                    field === "runPreLaunch"
                      ? "page.setting.xxmi.builtin.preLaunch"
                      : "page.setting.xxmi.builtin.postLoad",
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
              <ToggleRow
                label={t("page.setting.xxmi.builtin.customLaunch")}
                checked={config.customLaunch.enabled}
                onCheckedChange={(enabled) =>
                  setConfig({ ...config, customLaunch: { ...config.customLaunch, enabled } })
                }
              >
                <Input
                  aria-label={t("page.setting.xxmi.builtin.customLaunch")}
                  className="font-mono"
                  spellCheck={false}
                  value={config.customLaunch.command}
                  onChange={(event) =>
                    setConfig({
                      ...config,
                      customLaunch: { ...config.customLaunch, command: event.target.value },
                    })
                  }
                />
                <SelectRow
                  label={t("page.setting.xxmi.builtin.injectMode")}
                  value={config.customLaunch.injectMode}
                  options={["Hook", "Inject", "Bypass"]}
                  onValueChange={(injectMode) =>
                    setConfig({ ...config, customLaunch: { ...config.customLaunch, injectMode } })
                  }
                />
                <Alert>
                  <ShieldAlertIcon />
                  <AlertDescription>
                    {t("page.setting.xxmi.builtin.elevatedCommandWarning")}
                  </AlertDescription>
                </Alert>
              </ToggleRow>
              <ToggleRow
                label={t("page.setting.xxmi.builtin.extraLibraries")}
                description={
                  config.mode === RuntimeMode.RuntimeLegacy && !cachedLibs?.length
                    ? `${t("page.setting.xxmi.builtin.libs")}: ${t("page.setting.xxmi.builtin.notInstalled")}`
                    : undefined
                }
                checked={config.extraLibraries.enabled}
                disabled={
                  config.mode === RuntimeMode.RuntimeLegacy &&
                  !cachedLibs?.some(
                    (entry) =>
                      !config.xxmiVersion.pinned || entry.version === config.xxmiVersion.pinned,
                  )
                }
                onCheckedChange={(enabled) =>
                  setConfig({ ...config, extraLibraries: { ...config.extraLibraries, enabled } })
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
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>3DMigoto</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              {(
                [
                  "enforceRendering",
                  "enableHunting",
                  "dumpShaders",
                  "muteWarnings",
                  "callsLogging",
                  "debugLogging",
                  "unsafeMode",
                ] as const
              ).map((field) => (
                <ToggleRow
                  key={field}
                  label={t(`page.setting.xxmi.builtin.${field}`)}
                  description={
                    field === "unsafeMode" && config.migoto.unsafeMode
                      ? t("page.setting.xxmi.builtin.unsafeWarning")
                      : undefined
                  }
                  checked={config.migoto[field]}
                  onCheckedChange={(value) =>
                    setConfig({ ...config, migoto: { ...config.migoto, [field]: value } })
                  }
                />
              ))}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="tools" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>{t("page.setting.xxmi.builtin.iniOptimizer")}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <ToggleRow
                label={t("page.setting.xxmi.builtin.optimizeAtLaunch")}
                checked={config.iniOptimizer.enabled}
                onCheckedChange={(enabled) =>
                  setConfig({ ...config, iniOptimizer: { ...config.iniOptimizer, enabled } })
                }
              />
              <ToggleRow
                label={t("page.setting.xxmi.builtin.resetOptimizerCache")}
                checked={config.iniOptimizer.resetCache}
                onCheckedChange={(resetCache) =>
                  setConfig({ ...config, iniOptimizer: { ...config.iniOptimizer, resetCache } })
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
                <div className="max-h-64 space-y-1 overflow-y-auto rounded-md border p-2 font-mono text-xs">
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
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t("page.setting.xxmi.builtin.maintenance")}</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-wrap gap-2">
              <Button
                variant="outline"
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
                onClickPromise={async () => {
                  try {
                    await XXMI.CreateShortcut(importer);
                    refresh();
                    toast.success(t("page.setting.xxmi.builtin.shortcutCreated"));
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  }
                }}
              >
                {t("page.setting.xxmi.builtin.createShortcut")}
              </Button>
              {saved?.shortcutPath && (
                <Button
                  variant="outline"
                  onClickPromise={async () => {
                    try {
                      await XXMI.DeleteShortcut(importer);
                      refresh();
                      toast.success(t("page.setting.xxmi.builtin.shortcutDeleted"));
                    } catch (error) {
                      toast.error(toErrorMessage(error));
                    }
                  }}
                >
                  {t("page.setting.xxmi.builtin.deleteShortcut")}
                </Button>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

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

      <AlertDialog open={leaveOpen} onOpenChange={setLeaveOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.setting.xxmi.builtin.leaveTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.setting.xxmi.builtin.leaveDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void back()}>
              {t("page.setting.xxmi.builtin.leaveConfirm")}
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
}: {
  selected: boolean;
  onSelect: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-muted/60",
        selected && "bg-muted",
      )}
    >
      <CheckIcon className={cn("size-4 shrink-0", !selected && "invisible")} />
      {children}
    </button>
  );
}
