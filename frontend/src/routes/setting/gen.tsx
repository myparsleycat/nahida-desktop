import { Updater } from "@bindings/infra";
import { Setting } from "@bindings/setting";
import { type Theme, useTheme } from "@renderer/components/theme-provider";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Separator } from "@renderer/components/ui/separator";
import { Switch } from "@renderer/components/ui/switch";
import { useAuth } from "@renderer/hooks/use-auth";
import { useSettings } from "@renderer/hooks/use-settings";
import { getFallbackStartPage, requiresAuthForStartPage } from "@renderer/lib/start-page";
import { useGlobalStore } from "@renderer/store/global";
import type { AutoUpdateMode } from "@shared/updater";
import { formatSize } from "@shared/utils";
import { createFileRoute } from "@tanstack/react-router";
import { DownloadIcon, LoaderIcon, RefreshCwIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/gen")({
  component: RouteComponent,
});

const settingsConfig = {
  runOnStartup: "general.runOnStartup",
  language: "general.language",
  autoUpdateMode: "general.autoUpdateMode",
  runInBackground: "general.runInBackground",
  titlebarActivityBadgeClickNavigate: "general.titlebarActivityBadgeClickNavigate",
  defaultStartPage: "general.defaultStartPage",
  logLevel: "general.logLevel",
} as const;

function RouteComponent() {
  const { theme, setTheme } = useTheme();
  const { t } = useTranslation();
  const { session, sessionInitialized } = useAuth();
  const appStatus = useGlobalStore((state) => state.appStatus);
  const updateAvailable = useGlobalStore((state) => state.updateAvailable);
  const updateDownloaded = useGlobalStore((state) => state.updateDownloaded);
  const releaseVersion = useGlobalStore((state) => state.releaseVersion);
  const shouldPromptForUpdate = useGlobalStore((state) => state.shouldPromptForUpdate);
  const setShouldPromptForUpdate = useGlobalStore((state) => state.setShouldPromptForUpdate);
  const updaterMode = useGlobalStore((state) => state.updaterMode);
  const updaterChecking = useGlobalStore((state) => state.updaterChecking);
  const updaterDownloading = useGlobalStore((state) => state.updaterDownloading);
  const downloadWritten = useGlobalStore((state) => state.downloadWritten);
  const downloadTotal = useGlobalStore((state) => state.downloadTotal);
  const updaterLastError = useGlobalStore((state) => state.updaterLastError);

  const { settings, update, isLoading, setSettings } = useSettings(settingsConfig);

  const [imageCacheSize, setImageCacheSize] = useState<number | null>(null);
  const [isRunInBackgroundConfirmOpen, setIsRunInBackgroundConfirmOpen] = useState(false);
  const [isUpdaterActionPending, setIsUpdaterActionPending] = useState(false);

  useEffect(() => {
    void Setting.GetImageCacheSize().then((size) => {
      setImageCacheSize(size);
    });
  }, []);

  const handleRunInBackgroundChange = async (val: boolean) => {
    if (val) {
      await update("runInBackground", true);
      return;
    }

    const persistEnabled = await Setting.GetPersistToggles();

    if (persistEnabled) {
      setIsRunInBackgroundConfirmOpen(true);
      setSettings((prev) => ({ ...prev, runInBackground: true }));
      return;
    }

    await update("runInBackground", false);
  };

  const isLoggedIn = !!session;
  const startPageFallback = getFallbackStartPage();
  const startPageOptions = [
    { value: "/transfer", label: t("page.transfer.title") },
    { value: "/gamebanana", label: t("page.gamebanana.title") },
    { value: "/mod", label: t("page.mod.title") },
    {
      value: "/drive/drive/root",
      label: t("page.drive.title"),
      disabled: sessionInitialized && !isLoggedIn,
    },
    {
      value: "/drive/share/root",
      label: t("page.share_drive.title"),
      disabled: sessionInitialized && !isLoggedIn,
    },
  ];
  const selectedStartPage =
    sessionInitialized && !isLoggedIn && requiresAuthForStartPage(settings.defaultStartPage)
      ? startPageFallback
      : settings.defaultStartPage;

  const confirmDisableRunInBackground = async () => {
    setIsRunInBackgroundConfirmOpen(false);
    await update("runInBackground", false);
  };

  const languageOptions = [
    { value: "ko", label: "한국어" },
    { value: "en", label: "English" },
    { value: "ja", label: "日本語" },
    { value: "zh", label: "中文" },
  ] as const;

  const themeOptions = [
    { value: "system", label: t("page.setting.gen.theme.system") },
    { value: "light", label: t("page.setting.gen.theme.light") },
    { value: "dark", label: t("page.setting.gen.theme.dark") },
  ] as const;

  const logLevelOptions = [
    { value: "trace", label: "Trace" },
    { value: "debug", label: "Debug" },
    { value: "info", label: "Info" },
    { value: "warn", label: "Warn" },
    { value: "error", label: "Error" },
    { value: "fatal", label: "Fatal" },
  ] as const;

  const autoUpdateModeOptions: Array<{
    value: AutoUpdateMode;
    label: string;
    description: string;
  }> = [
    {
      value: "auto",
      label: t("page.setting.gen.application.autoUpdateModes.auto.title"),
      description: t("page.setting.gen.application.autoUpdateModes.auto.description"),
    },
    {
      value: "notify",
      label: t("page.setting.gen.application.autoUpdateModes.notify.title"),
      description: t("page.setting.gen.application.autoUpdateModes.notify.description"),
    },
    {
      value: "off",
      label: t("page.setting.gen.application.autoUpdateModes.off.title"),
      description: t("page.setting.gen.application.autoUpdateModes.off.description"),
    },
  ];

  const selectedAutoUpdateMode = autoUpdateModeOptions.find(
    (option) => option.value === settings.autoUpdateMode,
  );
  const versionRangeText =
    appStatus?.version && releaseVersion ? ` (${appStatus.version} → ${releaseVersion})` : "";

  const downloadPercent =
    updaterDownloading && downloadTotal > 0
      ? Math.round((downloadWritten / downloadTotal) * 100)
      : null;
  const updaterStatusText = updaterChecking
    ? t("updater.status.checking")
    : updaterDownloading
      ? downloadPercent !== null
        ? t("updater.status.downloadingProgress", { percent: downloadPercent })
        : t("updater.status.downloading")
      : updaterLastError
        ? t("updater.status.failed", { message: updaterLastError.message })
        : updateDownloaded
          ? t("updater.status.downloaded")
          : updateAvailable
            ? t("updater.status.available", { versionRangeText })
            : settings.autoUpdateMode === "off"
              ? t("page.setting.gen.application.autoUpdateModes.off.title")
              : t("updater.status.idle");

  const shouldOfferManualDownload =
    updateAvailable && !updateDownloaded && (shouldPromptForUpdate || updaterMode === "notify");

  const handleUpdateAction = async () => {
    setIsUpdaterActionPending(true);

    try {
      if (updateDownloaded) {
        setShouldPromptForUpdate(true);
        return;
      }

      if (shouldOfferManualDownload) {
        await Updater.DownloadUpdate();
      }
    } finally {
      setIsUpdaterActionPending(false);
    }
  };

  if (isLoading) {
    return null;
  }

  return (
    <main className="mx-auto flex w-full flex-1 flex-col space-y-6 p-4 select-none">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            {t("page.setting.gen.application.title")}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {appStatus?.supportsAutostart && (
            <>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <span className="text-sm font-medium">
                    {t("page.setting.gen.application.runOnStartup")}
                  </span>
                  <p className="text-xs text-muted-foreground">
                    {t("page.setting.gen.application.runOnStartupDescription")}
                  </p>
                </div>
                <Switch
                  checked={settings.runOnStartup}
                  onCheckedChange={(val) => update("runOnStartup", val)}
                />
              </div>

              <Separator />
            </>
          )}

          <div className="flex items-center justify-between space-x-3">
            <div className="flex-1 space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.gen.application.autoUpdate")}
              </span>
              <p className="text-xs text-muted-foreground">{selectedAutoUpdateMode?.description}</p>
            </div>
            <div className="flex items-center gap-4">
              <Select
                value={settings.autoUpdateMode}
                items={autoUpdateModeOptions}
                onValueChange={(val) => {
                  if (val === null) return;
                  void update("autoUpdateMode", val);
                }}
              >
                <SelectTrigger className="w-42">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent finalFocus={false}>
                  <SelectGroup>
                    {autoUpdateModeOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
          </div>

          <Separator />

          <div className="flex items-center justify-between gap-4">
            <div className="flex-1 space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.gen.application.updateStatus")}
              </span>
              <p className="text-xs text-muted-foreground">{updaterStatusText}</p>
            </div>
            {(shouldOfferManualDownload || updateDownloaded) && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                isLoading={isUpdaterActionPending || updaterChecking || updaterDownloading}
                onClick={handleUpdateAction}
              >
                {updateDownloaded ? (
                  <>
                    <RefreshCwIcon />
                    {t("updater.actions.install")}
                  </>
                ) : (
                  <>
                    <DownloadIcon />
                    {t("updater.actions.download")}
                  </>
                )}
              </Button>
            )}
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.gen.application.runInBackground")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.gen.application.runInBackgroundDescription")}
              </p>
            </div>
            <Switch
              checked={settings.runInBackground}
              onCheckedChange={handleRunInBackgroundChange}
            />
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.gen.application.titlebarActivityBadgeClickNavigate")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.gen.application.titlebarActivityBadgeClickNavigateDescription")}
              </p>
            </div>
            <Switch
              checked={settings.titlebarActivityBadgeClickNavigate}
              onCheckedChange={(val) => update("titlebarActivityBadgeClickNavigate", val)}
            />
          </div>
        </CardContent>
      </Card>

      <AlertDialog
        open={isRunInBackgroundConfirmOpen}
        onOpenChange={setIsRunInBackgroundConfirmOpen}
      >
        <AlertDialogContent className="w-full">
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("page.setting.gen.application.runInBackgroundDisableConfirmTitle")}
            </AlertDialogTitle>
          </AlertDialogHeader>
          <p className="text-sm text-pretty text-muted-foreground *:[a]:underline *:[a]:underline-offset-3 *:[a]:hover:text-foreground">
            {t("page.setting.gen.application.runInBackgroundDisableConfirmDescription")}
          </p>
          <AlertDialogFooter className="flex flex-row justify-end">
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={confirmDisableRunInBackground}>
              {t("page.setting.gen.application.runInBackgroundDisableConfirmAction")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Card>
        <CardContent>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-3">
              <label className="text-sm font-medium" htmlFor="language">
                {t("page.setting.gen.language.title")}
              </label>
              <Select
                name="language"
                value={settings.language}
                items={languageOptions}
                onValueChange={(val) => {
                  if (val === null) return;
                  void update("language", val);
                }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.language.select")} />
                </SelectTrigger>
                <SelectContent finalFocus={false}>
                  <SelectGroup>
                    {languageOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-3">
              <label className="text-sm font-medium" htmlFor="theme">
                {t("page.setting.gen.theme.title")}
              </label>
              <Select
                name="theme"
                value={theme}
                items={themeOptions}
                onValueChange={(v) => setTheme(v as Theme)}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.theme.select")} />
                </SelectTrigger>
                <SelectContent finalFocus={false}>
                  <SelectGroup>
                    {themeOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-3">
              <label className="text-sm font-medium" htmlFor="startPage">
                {t("page.setting.gen.startPage.title")}
              </label>
              <Select
                name="startPage"
                value={selectedStartPage}
                items={startPageOptions}
                onValueChange={(val) => {
                  if (val === null) return;
                  void update("defaultStartPage", val);
                }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.startPage.select")} />
                </SelectTrigger>
                <SelectContent finalFocus={false}>
                  <SelectGroup>
                    {startPageOptions.map((option) => (
                      <SelectItem
                        key={option.value}
                        value={option.value}
                        disabled={option.disabled}
                      >
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-3">
              <label className="text-sm font-medium" htmlFor="logLevel">
                {t("page.setting.gen.logLevel.title")}
              </label>
              <Select
                name="logLevel"
                value={settings.logLevel}
                items={logLevelOptions}
                onValueChange={(val) => {
                  if (val === null) return;
                  void update("logLevel", val);
                }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.logLevel.select")} />
                </SelectTrigger>
                <SelectContent finalFocus={false}>
                  <SelectGroup>
                    {logLevelOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">{t("page.setting.gen.other.title")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.gen.other.imageCacheTitle")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.gen.other.imageCacheDescription")}
              </p>
            </div>
            <div className="flex items-center gap-2">
              <p className="text-sm">
                {imageCacheSize === null ? (
                  <LoaderIcon className="size-4 animate-spin text-muted-foreground" />
                ) : (
                  formatSize(imageCacheSize)
                )}
              </p>
              <Button
                variant="outline"
                disabled={imageCacheSize === null}
                onClick={() => {
                  setImageCacheSize(null);
                  void Setting.ClearImageCache().then(() => {
                    void Setting.GetImageCacheSize().then((size) => {
                      setImageCacheSize(size);
                    });
                  });
                }}
              >
                {t("page.setting.gen.other.imageCacheClear")}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </main>
  );
}
