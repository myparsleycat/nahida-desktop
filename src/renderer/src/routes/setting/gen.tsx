import { Theme, useTheme } from "@renderer/components/theme-provider";
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
import { Switch } from "@renderer/components/ui/switch";
import { Separator } from "@renderer/components/ui/separator";
import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/gen")({
  component: RouteComponent,
});

function RouteComponent() {
  const { theme, setTheme } = useTheme();
  const { t } = useTranslation();

  const [runOnStartup, setRunOnStartup] = useState(false);
  const [language, setLanguage] = useState<string | undefined>(undefined);
  const [checkBackgroundUpdates, setCheckBackgroundUpdates] = useState(true);
  const [autoUpdate, setAutoUpdate] = useState(false);
  const [moveTransferPageWhenStartTransfer, setMoveTransferPageWhenStartTransfer] = useState(false);
  const [powerSaveBlockInTransfer, setPowerSaveBlockInTransfer] = useState(false);
  const [defaultStartPage, setDefaultStartPage] = useState<string>("/mod");

  useEffect(() => {
    window.api.invoke("setting:general:getRunOnStartup").then((val: boolean) => {
      setRunOnStartup(val);
    });

    window.api.invoke("setting:general:getLanguage").then((val) => {
      if (val) {
        setLanguage(val);
      }
    });

    window.api.invoke("setting:general:getCheckBackgroundUpdates").then((val: boolean) => {
      setCheckBackgroundUpdates(val);
    });

    window.api
      .invoke("setting:general:getMoveTransferPageWhenStartTransfer")
      .then((val: boolean) => {
        setMoveTransferPageWhenStartTransfer(val);
      });

    window.api.invoke("setting:general:getPowerSaveBlockInTransfer").then((val: boolean) => {
      setPowerSaveBlockInTransfer(val);
    });

    window.api.invoke("setting:general:getDefaultStartPage").then((val: string | null) => {
      setDefaultStartPage(val || "/mod");
    });
  }, []);

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
              checked={runOnStartup}
              onCheckedChange={(val) => {
                setRunOnStartup(val);
                window.api.invoke("setting:general:setRunOnStartup", val);
              }}
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
                size="sm"
                onClick={() => window.api.invoke("setting:general:checkUpdate")}
              >
                {t("page.setting.gen.application.checkUpdate")}
              </Button>
              <Switch
                checked={checkBackgroundUpdates}
                onCheckedChange={(val) => {
                  setCheckBackgroundUpdates(val);
                  window.api.invoke("setting:general:setCheckBackgroundUpdates", val);
                }}
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

      <div className="grid grid-cols-3 gap-3">
        <div className="space-y-3">
          <label className="text-sm font-medium">{t("page.setting.gen.language.title")}</label>
          <Select
            value={language}
            onValueChange={(val) => {
              setLanguage(val);
              window.api.invoke("setting:general:setLanguage", val);
            }}
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
          <label className="text-sm font-medium">{t("page.setting.gen.theme.title")}</label>
          <Select value={theme} onValueChange={(v) => setTheme(v as Theme)}>
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
          <label className="text-sm font-medium">{t("page.setting.gen.startPage.title")}</label>
          <Select
            value={defaultStartPage}
            onValueChange={(v) => {
              setDefaultStartPage(v);
              window.api.invoke("setting:general:setDefaultStartPage", v);
            }}
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
      </div>

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
              checked={moveTransferPageWhenStartTransfer}
              onCheckedChange={(val) => {
                setMoveTransferPageWhenStartTransfer(val);
                window.api.invoke("setting:general:setMoveTransferPageWhenStartTransfer", val);
              }}
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
              checked={powerSaveBlockInTransfer}
              onCheckedChange={(val) => {
                setPowerSaveBlockInTransfer(val);
                window.api.invoke("setting:general:setPowerSaveBlockInTransfer", val);
              }}
            />
          </div>
        </CardContent>
      </Card>
    </main>
  );
}
