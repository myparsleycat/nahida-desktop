import { Mod } from "@bindings/mod";
import { Dialog } from "@bindings/platform";
import { XXMI } from "@bindings/xxmi";
import {
  ImportUserDataMode,
  LauncherMode,
  RuntimeMode,
  type ImportExternalLauncherInput,
  type ImporterConfig,
} from "@bindings/xxmi/models";
import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Button } from "@renderer/components/ui/button";
import {
  Section,
  SectionAction,
  SectionContent,
  SectionHeader,
  SectionTitle,
} from "@renderer/components/ui/section";
import { CustomDLLField } from "@renderer/components/xxmi/xxmi-custom-dll";
import { XXMIExternalLauncher } from "@renderer/components/xxmi/xxmi-external-launcher";
import {
  FOLLOW_LATEST,
  PathField,
  SelectRow,
  ToggleRow,
} from "@renderer/components/xxmi/xxmi-fields";
import { XXMIImportDialog } from "@renderer/components/xxmi/xxmi-import-dialog";
import { installableUpdates, useXXMIUpdates } from "@renderer/components/xxmi/xxmi-importer-list";
import { useSettings } from "@renderer/hooks/use-settings";
import { cn } from "@renderer/lib/utils";
import { FileDropTargetID } from "@renderer/wails/file-drop";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
  DownloadIcon,
  FileArchiveIcon,
  InfoIcon,
  RotateCcwIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/xxmi/")({ component: XXMIDashboard });

export type XXMIData = Awaited<ReturnType<typeof XXMI.GetXXMIData>>;

// Keep in sync with the import errors in internal/xxmi/migrate_user_data.go.
const importErrorCodes = [
  "XXMI_IMPORT_FOLDER_CONFLICT",
  "XXMI_IMPORT_TARGET_NOT_EMPTY",
  "XXMI_IMPORT_NO_SPACE",
  "XXMI_IMPORT_VERSION_UNKNOWN",
  "XXMI_IMPORT_SOURCE_CHANGED",
  "XXMI_IMPORT_PREVIEW_REQUIRED",
  "XXMI_GAME_RUNNING",
] as const;
const settingsConfig = {
  autoUpdateMode: "xxmi.autoUpdate",
  includePrereleases: "xxmi.includePrereleases",
} as const;
const launchUpdateModes = ["auto", "notify", "off"] as const;

export function XXMIDashboard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { settings, update } = useSettings(settingsConfig);
  const { data: overview } = useQuery({ queryKey: ["xxmi:overview"], queryFn: XXMI.GetOverview });
  const external = overview?.launcherMode === LauncherMode.LauncherExternal;
  const libs = overview?.libsCache;
  const legacy = overview?.legacyRuntimes;
  const fpsVersions = overview?.fpsVersions;
  const { data: fpsReleases } = useQuery({
    queryKey: ["xxmi:fps-releases"],
    queryFn: () => XXMI.ListReleases("gi-fps-unlocker"),
    staleTime: 60 * 60 * 1000,
  });
  const { data: libsReleases } = useQuery({
    queryKey: ["xxmi:libs-releases"],
    queryFn: () => XXMI.ListReleases("xxmi-libs"),
  });
  const updates = useXXMIUpdates(!external && (overview?.configured ?? false));
  const [editedRoot, setEditedRoot] = useState<string | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  // Importers that follow the shared libraries but would ignore the shared custom DLL until unsafe mode is on.
  const [unsafePrompt, setUnsafePrompt] = useState<{ key: string; config: ImporterConfig }[]>([]);
  const sharedCustomDll = overview?.sharedCustomDll;
  const root = editedRoot ?? overview?.root ?? "";
  // A cleared root field imports into the saved root, which the overview already resolves to the default.
  const importRoot = root.trim() || overview?.root || "";
  const pendingUpdates = installableUpdates(updates);
  const latestFPS = fpsReleases?.[0];

  const refresh = () => {
    void queryClient.invalidateQueries({
      predicate: (query) =>
        typeof query.queryKey[0] === "string" && query.queryKey[0].startsWith("xxmi:"),
    });
  };

  const importExternal = async (input: ImportExternalLauncherInput) => {
    if (!overview?.externalLauncher) return;
    const userData = input.userData;
    const imported = await XXMI.ImportExternalLauncher(input).catch((error: unknown) => {
      const message = toErrorMessage(error);
      const code = importErrorCodes.find((code) => message.includes(code));
      toast.error(code ? t(`page.setting.xxmi.builtin.importErrors.${code}`) : message);
      return undefined;
    });
    if (imported === undefined) return;
    setImportOpen(false);
    setEditedRoot(null);
    refresh();
    void queryClient.invalidateQueries({ queryKey: ["settings"] });
    toast.success(t("page.setting.xxmi.builtin.imported"));
    if (userData !== ImportUserDataMode.ImportUserDataMove) return;

    // The import is already committed, so a game that fails to follow the moved Mods folder is reported on its own
    // instead of failing the import.
    const normalize = (path: string) =>
      path.replaceAll("/", "\\").replace(/\\+$/, "").toLowerCase();
    const games = await Mod.GetGames().catch(() => undefined);
    let failed = games === undefined;
    for (const importer of imported ?? []) {
      const oldMods = normalize(`${importer.previousFolder}\\Mods`);
      for (const game of (games ?? []).filter(
        (game) => normalize(game.modFolderPath) === oldMods,
      )) {
        await Mod.UpdateGame(game.game, {
          modFolderPath: `${importer.importerFolder}\\Mods`,
          importer: game.importer,
          linkedModFolderPath: game.linkedModFolderPath,
          gameInstallPath: game.gameInstallPath,
          gameExecutablePath: game.gameExecutablePath,
        }).catch(() => {
          failed = true;
        });
      }
    }
    void queryClient.invalidateQueries({ queryKey: ["games"] });
    if (failed) toast.warning(t("page.setting.xxmi.builtin.importGamesFailed"));
  };

  const selectSharedCustomDll = async (path: string) => {
    const dll = await XXMI.ImportCustomDLL(path);
    await XXMI.SetSharedCustomDLL(dll.id);
    refresh();
    toast.success(t("page.setting.xxmi.builtin.customDllSet"));

    const configs = await Promise.all(
      (overview?.importers ?? []).map(async (entry) => ({
        key: entry.key,
        config: await XXMI.GetImporterConfig(entry.key),
      })),
    );
    setUnsafePrompt(
      configs.filter(
        ({ config }) =>
          config.mode === RuntimeMode.RuntimeXXMI &&
          config.xxmiVersion.follow === "shared" &&
          !config.migoto.unsafeMode,
      ),
    );
  };

  const enableUnsafeMode = async () => {
    try {
      for (const { key, config } of unsafePrompt) {
        await XXMI.SaveImporterConfig(key, {
          ...config,
          migoto: { ...config.migoto, unsafeMode: true },
        });
      }
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
    setUnsafePrompt([]);
    refresh();
  };

  const resetBuiltin = async () => {
    try {
      await XXMI.ResetBuiltinRuntime();
      setResetOpen(false);
      setEditedRoot(null);
      refresh();
      void queryClient.invalidateQueries({ queryKey: ["settings"] });
      toast.success(t("page.setting.xxmi.builtin.resetDone"));
    } catch (error) {
      const message = toErrorMessage(error);
      toast.error(
        message.includes("XXMI_BUSY") ? t("page.setting.xxmi.builtin.resetBusy") : message,
      );
    }
  };

  return (
    <main className="min-h-0 flex-1 scrollbar-gutter-stable overflow-y-auto">
      <div className="mx-auto w-full max-w-2xl p-4">
        {external ? (
          <div className="space-y-6 text-sm">
            <XXMIExternalLauncher />
          </div>
        ) : (
          <div className="flex flex-col gap-6">
            {pendingUpdates.length > 0 && (
              <Section>
                <SectionHeader>
                  <SectionTitle>
                    {t("page.setting.xxmi.builtin.updatesPending", {
                      count: pendingUpdates.length,
                    })}
                  </SectionTitle>
                  <SectionAction>
                    <Button
                      size="sm"
                      onClickPromise={async () => {
                        try {
                          await XXMI.InstallUpdates(
                            "",
                            pendingUpdates.map((entry) => entry.package),
                          );
                          refresh();
                        } catch (error) {
                          toast.error(toErrorMessage(error));
                        }
                      }}
                    >
                      <DownloadIcon />
                      {t("page.setting.xxmi.builtin.installUpdates")}
                    </Button>
                  </SectionAction>
                </SectionHeader>
                <SectionContent className="border-primary *:py-2">
                  {pendingUpdates.map((entry) => (
                    <div key={entry.package} className="flex items-center justify-between gap-3">
                      <span className="min-w-0 font-mono text-xs break-all text-muted-foreground">
                        {entry.package}:{" "}
                        {entry.installed || t("page.setting.xxmi.builtin.notInstalled")} →{" "}
                        {entry.latestVersion}
                      </span>
                      <Button
                        variant="ghost"
                        size="xs"
                        onClickPromise={async () => {
                          try {
                            await XXMI.SkipVersion(entry.package, entry.latestVersion);
                            refresh();
                          } catch (error) {
                            toast.error(toErrorMessage(error));
                          }
                        }}
                      >
                        {t("page.setting.xxmi.builtin.skipVersion")}
                      </Button>
                    </div>
                  ))}
                </SectionContent>
              </Section>
            )}

            <Section>
              <SectionHeader>
                <SectionTitle>{t("page.setting.xxmi.builtin.options")}</SectionTitle>
              </SectionHeader>
              <SectionContent>
                <PathField
                  label={t("page.setting.xxmi.builtin.root")}
                  description={t("page.setting.xxmi.builtin.rootDescription")}
                  value={root}
                  onValueChange={setEditedRoot}
                >
                  <Button
                    disabled={!root || root === overview?.root}
                    onClickPromise={async () => {
                      try {
                        await XXMI.SetRoot(root);
                        setEditedRoot(null);
                        refresh();
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    {t("g.save")}
                  </Button>
                </PathField>
                {overview?.externalLauncher && (
                  <Alert>
                    <InfoIcon />
                    <AlertTitle>{t("page.setting.xxmi.builtin.externalDetected")}</AlertTitle>
                    <AlertDescription className="space-y-2">
                      <p className="font-mono text-xs break-all">
                        {overview.externalLauncher.path}
                      </p>
                      <p>{t("page.setting.xxmi.builtin.externalWarning")}</p>
                      <Button variant="outline" size="sm" onClick={() => setImportOpen(true)}>
                        {t("page.setting.xxmi.builtin.import")}
                      </Button>
                    </AlertDescription>
                  </Alert>
                )}
                <SelectRow
                  label={t("page.setting.xxmi.builtin.launchUpdate")}
                  value={settings?.autoUpdateMode ?? "auto"}
                  options={launchUpdateModes.map((mode) => ({
                    value: mode,
                    label: t(`page.setting.xxmi.builtin.launchUpdateModes.${mode}`),
                  }))}
                  onValueChange={(value) => {
                    const mode = launchUpdateModes.find((mode) => mode === value);
                    if (mode) void update("autoUpdateMode", mode);
                  }}
                />
                <ToggleRow
                  label={t("page.setting.xxmi.builtin.prereleases")}
                  description={t("page.setting.xxmi.builtin.prereleasesDescription")}
                  checked={settings?.includePrereleases ?? false}
                  onCheckedChange={(value) => update("includePrereleases", value)}
                />
                <SelectRow
                  label={t("page.setting.xxmi.builtin.sharedLibsVersion")}
                  description={t("page.setting.xxmi.builtin.sharedLibsVersionDescription")}
                  value={overview?.sharedLibsVersion || FOLLOW_LATEST}
                  options={[
                    { value: FOLLOW_LATEST, label: t("page.setting.xxmi.builtin.latest") },
                    ...(libsReleases?.map((release) => release.version) ?? []),
                  ]}
                  onValueChange={(value) => {
                    void XXMI.SetSharedLibsVersion(value === FOLLOW_LATEST ? "" : value).then(
                      refresh,
                      (error: unknown) => toast.error(toErrorMessage(error)),
                    );
                  }}
                />
              </SectionContent>
            </Section>

            <Section>
              <SectionHeader>
                <SectionTitle>{t("page.setting.xxmi.builtin.packages")}</SectionTitle>
              </SectionHeader>
              <SectionContent>
                {overview?.cacheIssues?.map((issue) => (
                  <Alert key={issue} variant="destructive">
                    <TriangleAlertIcon />
                    <AlertTitle>{t("page.setting.xxmi.builtin.cacheIssue")}</AlertTitle>
                    <AlertDescription className="break-all">{issue}</AlertDescription>
                  </Alert>
                ))}
                <PackageRow
                  title={t("page.setting.xxmi.builtin.libs")}
                  versions={libs?.map((entry) => ({
                    key: entry.version,
                    label: entry.inUse
                      ? `${entry.version} · ${t("page.setting.xxmi.builtin.inUse")}`
                      : entry.version,
                    active: entry.inUse,
                  }))}
                  footer={
                    <CustomDLLField
                      dropTargetId={FileDropTargetID.xxmiSharedCustomDll}
                      selected={
                        sharedCustomDll
                          ? {
                              id: sharedCustomDll,
                              name: overview?.customDlls?.find((dll) => dll.id === sharedCustomDll)
                                ?.name,
                            }
                          : undefined
                      }
                      onPick={selectSharedCustomDll}
                      onClear={async () => {
                        await XXMI.SetSharedCustomDLL("");
                        refresh();
                        toast.success(t("page.setting.xxmi.builtin.customDllCleared"));
                      }}
                    />
                  }
                >
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!libs?.some((entry) => !entry.referenced)}
                    onClickPromise={async () => {
                      try {
                        const removed = await XXMI.PruneLibsCache();
                        refresh();
                        toast.success(
                          t("page.setting.xxmi.builtin.pruned", { count: removed?.length ?? 0 }),
                        );
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    <Trash2Icon />
                    {t("page.setting.xxmi.builtin.prune")}
                  </Button>
                </PackageRow>
                <PackageRow
                  title={t("page.setting.xxmi.builtin.legacy")}
                  versions={legacy?.map((entry) => ({ key: entry.id, label: entry.id }))}
                >
                  <Button
                    variant="outline"
                    size="sm"
                    onClickPromise={async () => {
                      try {
                        const selected = await Dialog.ShowOpenDialog({
                          title: t("page.setting.xxmi.builtin.importLegacy"),
                          defaultPath: "",
                          filters: [{ name: "ZIP", extensions: ["zip"] }],
                          properties: ["openFile"],
                        });
                        if (selected.canceled || !selected.filePaths?.[0]) return;
                        await XXMI.ImportLegacyRuntimeZip(selected.filePaths[0]);
                        refresh();
                        toast.success(t("page.setting.xxmi.builtin.importedLegacy"));
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    <FileArchiveIcon />
                    {t("page.setting.xxmi.builtin.importLegacy")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClickPromise={async () => {
                      try {
                        await XXMI.UpdateLegacyRuntime();
                        refresh();
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    <DownloadIcon />
                    {t("page.setting.xxmi.builtin.downloadLegacy")}
                  </Button>
                </PackageRow>
                <PackageRow
                  title={t("page.setting.xxmi.builtin.fpsUnlocker")}
                  versions={fpsVersions?.map((version) => ({ key: version, label: version }))}
                >
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!latestFPS || fpsVersions?.includes(latestFPS.version)}
                    onClickPromise={async () => {
                      if (!latestFPS) return;
                      try {
                        await XXMI.EnsureFPSUnlockerVersion(latestFPS.version);
                        refresh();
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    <DownloadIcon />
                    {t("page.setting.xxmi.builtin.downloadFPS")}
                  </Button>
                </PackageRow>
              </SectionContent>
            </Section>

            <Section>
              <SectionHeader>
                <SectionTitle>{t("page.setting.xxmi.builtin.reset")}</SectionTitle>
              </SectionHeader>
              <SectionContent>
                <div className="flex items-center justify-between gap-4">
                  <p className="text-xs text-muted-foreground">
                    {t("page.setting.xxmi.builtin.resetDescription")}
                  </p>
                  <Button
                    variant="destructive"
                    className="shrink-0"
                    disabled={!overview}
                    onClick={() => setResetOpen(true)}
                  >
                    <RotateCcwIcon />
                    {t("page.setting.xxmi.builtin.resetConfirm")}
                  </Button>
                </div>
              </SectionContent>
            </Section>
          </div>
        )}
        <AlertDialog open={resetOpen} onOpenChange={setResetOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t("page.setting.xxmi.builtin.resetTitle")}</AlertDialogTitle>
              <AlertDialogDescription>
                {t("page.setting.xxmi.builtin.resetConfirmDescription")}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
              <Button variant="destructive" onClickPromise={resetBuiltin}>
                {t("page.setting.xxmi.builtin.resetConfirm")}
              </Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
        <AlertDialog
          open={unsafePrompt.length > 0}
          onOpenChange={(open) => {
            if (!open) setUnsafePrompt([]);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t("page.setting.xxmi.builtin.customDllEnableUnsafeTitle")}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t("page.setting.xxmi.builtin.customDllEnableUnsafeShared", {
                  importers: unsafePrompt.map((entry) => entry.key).join(", "),
                })}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
              <Button onClickPromise={enableUnsafeMode}>
                {t("page.setting.xxmi.builtin.customDllEnableUnsafeConfirm")}
              </Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
        {importOpen && overview?.externalLauncher && (
          <XXMIImportDialog
            path={overview.externalLauncher.path}
            root={importRoot}
            onImport={importExternal}
            onClose={() => setImportOpen(false)}
          />
        )}
      </div>
    </main>
  );
}

function PackageRow({
  title,
  versions,
  footer,
  children,
}: {
  title: string;
  versions?: { key: string; label: string; active?: boolean }[];
  footer?: ReactNode;
  children: ReactNode;
}) {
  const { t } = useTranslation();

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-3">
        <span className="font-medium">{title}</span>
        <div className="flex flex-wrap justify-end gap-2">{children}</div>
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1">
        {versions?.length ? (
          versions.map((version) => (
            <span
              key={version.key}
              className={cn(
                "font-mono text-xs",
                version.active ? "font-medium text-foreground" : "text-muted-foreground",
              )}
            >
              {version.label}
            </span>
          ))
        ) : (
          <span className="text-xs text-muted-foreground">
            {t("page.setting.xxmi.builtin.notInstalled")}
          </span>
        )}
      </div>
      {footer}
    </div>
  );
}
