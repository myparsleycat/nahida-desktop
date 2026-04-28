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
import { useSettings } from "@renderer/hooks/use-settings";
import { useAuth } from "@renderer/hooks/use-auth";
import { getFallbackStartPage, requiresAuthForStartPage } from "@renderer/lib/start-page";
import { useGlobalStore } from "@renderer/store/global";
import { supportsWindowsDesktopFeatures } from "@shared/platform";
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
  runOnStartup: "setting:general:getRunOnStartup",
  language: "setting:general:getLanguage",
  autoUpdateMode: "setting:general:getAutoUpdateMode",
  runInBackground: "setting:general:getRunInBackground",
  defaultStartPage: "setting:general:getDefaultStartPage",
  titlebarStyle: "setting:general:getTitlebarStyle",
  logLevel: "setting:general:getLogLevel",
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
  const updaterMode = useGlobalStore((state) => state.updaterMode);
  const updaterChecking = useGlobalStore((state) => state.updaterChecking);
  const updaterDownloading = useGlobalStore((state) => state.updaterDownloading);
  const hasWindowsDesktopFeatures = supportsWindowsDesktopFeatures(appStatus?.platform);

  const { settings, update, isLoading, setSettings } = useSettings<{
    runOnStartup: boolean;
    language: string;
    autoUpdateMode: AutoUpdateMode;
    runInBackground: boolean;
    defaultStartPage: string;
    titlebarStyle: string;
    logLevel: string;
  }>(settingsConfig);

  const [imageCacheSize, setImageCacheSize] = useState<number | null>(null);
  const [isRunInBackgroundConfirmOpen, setIsRunInBackgroundConfirmOpen] = useState(false);
  const [isUpdaterActionPending, setIsUpdaterActionPending] = useState(false);

  useEffect(() => {
    window.api.invoke("setting:general:getImageCacheSize").then((size) => {
      setImageCacheSize(size);
    });
  }, []);

  const handleRunInBackgroundChange = async (val: boolean) => {
    if (val) {
      await update("runInBackground", true, "setting:general:setRunInBackground");
      return;
    }

    if (!hasWindowsDesktopFeatures) {
      await update("runInBackground", false, "setting:general:setRunInBackground");
      return;
    }

    const [persistEnabled, toggleViewerEnabled] = await Promise.all([
      window.api.invoke("setting:xxmi:getPersistToggles"),
      window.api.invoke("setting:xxmi:getToggleViewerAutoGenerate"),
    ]);

    if (persistEnabled || toggleViewerEnabled) {
      setIsRunInBackgroundConfirmOpen(true);
      setSettings((prev) => ({ ...prev, runInBackground: true }));
      return;
    }

    await update("runInBackground", false, "setting:general:setRunInBackground");
  };

  const isLoggedIn = !!session;
  const startPageFallback = getFallbackStartPage(appStatus?.platform);
  const startPageOptions = [
    { value: "/transfer", label: t("page.transfer.title") },
    { value: "/gamebanana", label: t("page.gamebanana.title") },
    ...(hasWindowsDesktopFeatures ? [{ value: "/mod", label: t("page.mod.title") }] : []),
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
    await update("runInBackground", false, "setting:general:setRunInBackground");
  };

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

  const updaterStatusText = updaterChecking
    ? t("updater.status.checking")
    : updaterDownloading
      ? t("updater.status.downloading")
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
        await window.api.invoke("updater:installUpdate");
        return;
      }

      if (shouldOfferManualDownload) {
        await window.api.invoke("updater:downloadUpdate");
      }
    } finally {
      setIsUpdaterActionPending(false);
    }
  };

  if (isLoading) {
    return null;
  }

  return (
    <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            {t("page.setting.gen.application.title")}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
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
              onCheckedChange={(val) =>
                update("runOnStartup", val, "setting:general:setRunOnStartup")
              }
            />
          </div>

          <Separator />

          <div className="flex items-center justify-between space-x-3">
            <div className="space-y-0.5 flex-1">
              <span className="text-sm font-medium">
                {t("page.setting.gen.application.autoUpdate")}
              </span>
              <p className="text-xs text-muted-foreground">{selectedAutoUpdateMode?.description}</p>
            </div>
            <div className="flex items-center gap-4">
              <Select
                value={settings.autoUpdateMode}
                onValueChange={(val: AutoUpdateMode) =>
                  update("autoUpdateMode", val, "setting:general:setAutoUpdateMode")
                }
              >
                <SelectTrigger className="w-42">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
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
            <div className="space-y-0.5 flex-1">
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
          <p className="text-muted-foreground *:[a]:hover:text-foreground text-sm text-pretty *:[a]:underline *:[a]:underline-offset-3">
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
          <div className="grid grid-cols-3 gap-3">
            <div className="space-y-3">
              <label className="text-sm font-medium" htmlFor="language">
                {t("page.setting.gen.language.title")}
              </label>
              <Select
                name="language"
                value={settings.language}
                onValueChange={(val) => update("language", val, "setting:general:setLanguage")}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.language.select")} />
                </SelectTrigger>
                <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
                  <SelectGroup>
                    <SelectItem value="ko">한국어</SelectItem>
                    <SelectItem value="en">English</SelectItem>
                    <SelectItem value="ja">日本語</SelectItem>
                    <SelectItem value="zh">中文</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-3">
              <label className="text-sm font-medium" htmlFor="theme">
                {t("page.setting.gen.theme.title")}
              </label>
              <Select name="theme" value={theme} onValueChange={(v) => setTheme(v as Theme)}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.theme.select")} />
                </SelectTrigger>
                <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
                  <SelectGroup>
                    <SelectItem value="system">{t("page.setting.gen.theme.system")}</SelectItem>
                    <SelectItem value="light">{t("page.setting.gen.theme.light")}</SelectItem>
                    <SelectItem value="dark">{t("page.setting.gen.theme.dark")}</SelectItem>
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
                onValueChange={(val) =>
                  update("defaultStartPage", val, "setting:general:setDefaultStartPage")
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.startPage.select")} />
                </SelectTrigger>
                <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
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
              <label className="text-sm font-medium" htmlFor="titlebarStyle">
                {t("page.setting.gen.titlebarStyle.title")}
              </label>
              <Select
                name="titlebarStyle"
                value={settings.titlebarStyle}
                onValueChange={(val) =>
                  update("titlebarStyle", val, "setting:general:setTitlebarStyle")
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.titlebarStyle.select")} />
                </SelectTrigger>
                <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
                  <SelectGroup>
                    <SelectItem value="modern">
                      {t("page.setting.gen.titlebarStyle.modern")}
                    </SelectItem>
                    <SelectItem value="native">
                      {t("page.setting.gen.titlebarStyle.native")}
                    </SelectItem>
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
                onValueChange={(val) => update("logLevel", val, "setting:general:setLogLevel")}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.logLevel.select")} />
                </SelectTrigger>
                <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
                  <SelectGroup>
                    <SelectItem value="trace">Trace</SelectItem>
                    <SelectItem value="debug">Debug</SelectItem>
                    <SelectItem value="info">Info</SelectItem>
                    <SelectItem value="warn">Warn</SelectItem>
                    <SelectItem value="error">Error</SelectItem>
                    <SelectItem value="fatal">Fatal</SelectItem>
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
                  window.api.invoke("setting:general:clearImageCache").then(() => {
                    window.api.invoke("setting:general:getImageCacheSize").then((size) => {
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
