import { type Theme, useTheme } from "@renderer/components/theme-provider";
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
import { formatSize } from "@shared/utils";
import { createFileRoute } from "@tanstack/react-router";
import { LoaderIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/gen")({
  component: RouteComponent,
});

const settingsConfig = {
  runOnStartup: "setting:general:getRunOnStartup",
  language: "setting:general:getLanguage",
  checkBackgroundUpdates: "setting:general:getCheckBackgroundUpdates",
  moveTransferPageWhenStartTransfer: "setting:general:getMoveTransferPageWhenStartTransfer",
  powerSaveBlockInTransfer: "setting:general:getPowerSaveBlockInTransfer",
  defaultStartPage: "setting:general:getDefaultStartPage",
  titlebarStyle: "setting:general:getTitlebarStyle",
  logLevel: "setting:general:getLogLevel",
} as const;

function RouteComponent() {
  const { theme, setTheme } = useTheme();
  const { t } = useTranslation();

  const { settings, update, isLoading } = useSettings<{
    runOnStartup: boolean;
    language: string;
    checkBackgroundUpdates: boolean;
    moveTransferPageWhenStartTransfer: boolean;
    powerSaveBlockInTransfer: boolean;
    defaultStartPage: string;
    titlebarStyle: string;
    logLevel: string;
  }>(settingsConfig);

  const [imageCacheSize, setImageCacheSize] = useState<number | null>(null);

  useEffect(() => {
    window.api.invoke("setting:general:getImageCacheSize").then((size) => {
      setImageCacheSize(size);
    });
  }, []);

  const [autoUpdate, setAutoUpdate] = useState(false);

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

          <div className="flex items-center justify-between">
            <div className="space-y-0.5 flex-1">
              <span className="text-sm font-medium">
                {t("page.setting.gen.application.checkBackgroundUpdates")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.gen.application.checkBackgroundUpdatesDescription")}
              </p>
            </div>
            <div className="flex items-center gap-4">
              <Button
                variant="secondary"
                onClick={() => window.api.invoke("setting:general:checkUpdate")}
              >
                {t("page.setting.gen.application.checkUpdate")}
              </Button>
              <Switch
                checked={settings.checkBackgroundUpdates}
                onCheckedChange={(val) =>
                  update("checkBackgroundUpdates", val, "setting:general:setCheckBackgroundUpdates")
                }
              />
            </div>
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <span className="text-sm font-medium text-muted-foreground">
                {t("page.setting.gen.application.autoUpdate")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.gen.application.autoUpdateDescription")}
              </p>
            </div>
            <Switch checked={autoUpdate} onCheckedChange={setAutoUpdate} disabled />
          </div>
        </CardContent>
      </Card>

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
                value={settings.defaultStartPage}
                onValueChange={(val) =>
                  update("defaultStartPage", val, "setting:general:setDefaultStartPage")
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.setting.gen.startPage.select")} />
                </SelectTrigger>
                <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
                  <SelectGroup>
                    <SelectItem value="/transfer">{t("page.transfer.title")}</SelectItem>
                    <SelectItem value="/drive/drive/root">{t("page.drive.title")}</SelectItem>
                    <SelectItem value="/drive/share/root">{t("page.share_drive.title")}</SelectItem>
                    <SelectItem value="/mod">{t("page.mod.title")}</SelectItem>
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
                {t("page.setting.gen.other.moveTransferPageWhenStartTransfer")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.gen.other.moveTransferPageWhenStartTransferDescription")}
              </p>
            </div>
            <Switch
              checked={settings.moveTransferPageWhenStartTransfer}
              onCheckedChange={(val) =>
                update(
                  "moveTransferPageWhenStartTransfer",
                  val,
                  "setting:general:setMoveTransferPageWhenStartTransfer",
                )
              }
            />
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.gen.other.powerSaveBlockInTransfer")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.gen.other.powerSaveBlockInTransferDescription")}
              </p>
            </div>
            <Switch
              checked={settings.powerSaveBlockInTransfer}
              onCheckedChange={(val) =>
                update(
                  "powerSaveBlockInTransfer",
                  val,
                  "setting:general:setPowerSaveBlockInTransfer",
                )
              }
            />
          </div>

          <Separator />

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
