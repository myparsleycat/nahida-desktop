import { Mod } from "@bindings/mod";
import { Dialog } from "@bindings/platform";
import { XXMI } from "@bindings/xxmi";
import { ImportUserDataMode, LauncherMode } from "@bindings/xxmi/models";
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
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@renderer/components/ui/card";
import { Separator } from "@renderer/components/ui/separator";
import { XXMIExternalLauncher } from "@renderer/components/xxmi/xxmi-external-launcher";
import { PathField, ToggleRow } from "@renderer/components/xxmi/xxmi-fields";
import { installableUpdates, useXXMIUpdates } from "@renderer/components/xxmi/xxmi-importer-list";
import { useSettings } from "@renderer/hooks/use-settings";
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
  "XXMI_IMPORT_MOVE_CROSS_VOLUME",
  "XXMI_IMPORT_VERSION_UNKNOWN",
  "XXMI_GAME_RUNNING",
] as const;
const settingsConfig = {
  autoUpdate: "xxmi.autoUpdate",
  includePrereleases: "xxmi.includePrereleases",
} as const;

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
  const updates = useXXMIUpdates(!external && (overview?.configured ?? false));
  const [editedRoot, setEditedRoot] = useState<string | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
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

  const importExternal = async (userData: ImportUserDataMode) => {
    if (!overview?.externalLauncher) return;
    const imported = await XXMI.ImportExternalLauncher({
      path: overview.externalLauncher.path,
      root: importRoot,
      userData,
    }).catch((error: unknown) => {
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
    <main className="mx-auto w-full max-w-6xl p-4">
      {external ? (
        <Card className="mx-auto max-w-2xl">
          <CardContent className="space-y-6">
            <XXMIExternalLauncher />
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-6">
          {pendingUpdates.length > 0 && (
            <Card className="border-primary/40 bg-primary/5">
              <CardHeader>
                <CardTitle>
                  {t("page.setting.xxmi.builtin.updatesPending", { count: pendingUpdates.length })}
                </CardTitle>
                <CardAction>
                  <Button
                    size="sm"
                    onClickPromise={async () => {
                      try {
                        await XXMI.InstallUpdates(pendingUpdates.map((entry) => entry.package));
                        refresh();
                        toast.success(t("page.setting.xxmi.builtin.updatesInstalled"));
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    <DownloadIcon />
                    {t("page.setting.xxmi.builtin.installUpdates")}
                  </Button>
                </CardAction>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
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
                          toast.success(t("page.setting.xxmi.builtin.versionSkipped"));
                        } catch (error) {
                          toast.error(toErrorMessage(error));
                        }
                      }}
                    >
                      {t("page.setting.xxmi.builtin.skipVersion")}
                    </Button>
                  </div>
                ))}
              </CardContent>
            </Card>
          )}

          <div className="grid items-start gap-6 xl:grid-cols-2">
            <div className="space-y-6">
              <Card>
                <CardHeader>
                  <CardTitle>{t("page.setting.xxmi.builtin.options")}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <PathField
                    label={t("page.setting.xxmi.builtin.root")}
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
                          toast.success(t("page.setting.xxmi.builtin.saved"));
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
                  <Separator />
                  <ToggleRow
                    label={t("page.setting.xxmi.builtin.autoUpdate")}
                    checked={settings?.autoUpdate ?? false}
                    onCheckedChange={(value) => update("autoUpdate", value)}
                  />
                  <ToggleRow
                    label={t("page.setting.xxmi.builtin.prereleases")}
                    checked={settings?.includePrereleases ?? false}
                    onCheckedChange={(value) => update("includePrereleases", value)}
                  />
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>{t("page.setting.xxmi.builtin.reset")}</CardTitle>
                  <CardDescription>
                    {t("page.setting.xxmi.builtin.resetDescription")}
                  </CardDescription>
                  <CardAction>
                    <Button
                      variant="destructive"
                      disabled={!overview}
                      onClick={() => setResetOpen(true)}
                    >
                      <RotateCcwIcon />
                      {t("page.setting.xxmi.builtin.resetConfirm")}
                    </Button>
                  </CardAction>
                </CardHeader>
              </Card>
            </div>

            <Card>
              <CardHeader>
                <CardTitle>{t("page.setting.xxmi.builtin.packages")}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4 text-sm">
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
                    label: entry.referenced
                      ? `${entry.version} · ${t("page.setting.xxmi.builtin.inUse")}`
                      : entry.version,
                    active: entry.referenced,
                  }))}
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
                <Separator />
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
                        toast.success(t("page.setting.xxmi.builtin.downloaded"));
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    <DownloadIcon />
                    {t("page.setting.xxmi.builtin.downloadLegacy")}
                  </Button>
                </PackageRow>
                <Separator />
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
                        toast.success(t("page.setting.xxmi.builtin.downloaded"));
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    <DownloadIcon />
                    {t("page.setting.xxmi.builtin.downloadFPS")}
                  </Button>
                </PackageRow>
              </CardContent>
            </Card>
          </div>
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
      <AlertDialog open={importOpen} onOpenChange={setImportOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("page.setting.xxmi.builtin.importUserDataTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.setting.xxmi.builtin.importUserDataDescription", { root: importRoot })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
            <li>{t("page.setting.xxmi.builtin.importUserDataKeepHint")}</li>
            <li>{t("page.setting.xxmi.builtin.importUserDataMoveHint")}</li>
          </ul>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <Button
              variant="outline"
              onClickPromise={() => importExternal(ImportUserDataMode.ImportUserDataKeep)}
            >
              {t("page.setting.xxmi.builtin.importUserDataKeep")}
            </Button>
            <Button onClickPromise={() => importExternal(ImportUserDataMode.ImportUserDataMove)}>
              {t("page.setting.xxmi.builtin.importUserDataMove")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  );
}

function PackageRow({
  title,
  versions,
  children,
}: {
  title: string;
  versions?: { key: string; label: string; active?: boolean }[];
  children: ReactNode;
}) {
  const { t } = useTranslation();

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-3">
        <span className="font-medium">{title}</span>
        <div className="flex flex-wrap justify-end gap-2">{children}</div>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {versions?.length ? (
          versions.map((version) => (
            <Badge
              key={version.key}
              variant={version.active ? "secondary" : "outline"}
              className="font-mono"
            >
              {version.label}
            </Badge>
          ))
        ) : (
          <span className="text-xs text-muted-foreground">
            {t("page.setting.xxmi.builtin.notInstalled")}
          </span>
        )}
      </div>
    </div>
  );
}
