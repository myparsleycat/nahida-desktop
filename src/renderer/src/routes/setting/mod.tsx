import { useAutoAnimate } from "@formkit/auto-animate/react";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import { Separator } from "@renderer/components/ui/separator";
import { Switch } from "@renderer/components/ui/switch";
import { useSettings } from "@renderer/hooks/use-settings";
import { Logger } from "@renderer/lib/logger";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/mod")({
  component: RouteComponent,
});

const settingsConfig = {
  deleteArchiveAfterExtract: "setting:mod:getDeleteArchiveAfterExtract",
  moveFolderInsteadOfCopy: "setting:mod:getMoveFolderInsteadOfCopy",
  virtualizationEnabled: "setting:mod:getVirtualizationEnabled",
  virtualizationThreshold: "setting:mod:getVirtualizationThreshold",
  searchModPreview: "setting:mod:getSearchModPreview",
} as const;

function RouteComponent() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [anim1] = useAutoAnimate({ duration: 150 });

  const { settings, update, setSettings, isLoading } = useSettings<{
    deleteArchiveAfterExtract: boolean;
    moveFolderInsteadOfCopy: boolean;
    virtualizationEnabled: boolean;
    virtualizationThreshold: number;
    searchModPreview: boolean;
  }>(settingsConfig);

  if (isLoading) {
    return null;
  }

  const handleVirtualizationEnabledChange = async (checked: boolean) => {
    try {
      await update("virtualizationEnabled", checked, "setting:mod:setVirtualizationEnabled");
      queryClient.invalidateQueries({ queryKey: ["settings", "mod", "virtualization"] });
    } catch (error) {
      Logger.error(error, "ModSettings:handleVirtualizationEnabledChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  const handleVirtualizationThresholdChange = async (value: number) => {
    if (value < 10) {
      toast.warning("기준 모드 개수는 10개 이상이어야 합니다.");
      return;
    }

    try {
      await update("virtualizationThreshold", value, "setting:mod:setVirtualizationThreshold");
      toast.success("설정이 저장되었습니다.");
      queryClient.invalidateQueries({ queryKey: ["settings", "mod", "virtualization"] });
    } catch (error) {
      Logger.error(error, "ModSettings:handleVirtualizationThresholdChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  return (
    <div className="space-y-6 p-4">
      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">
              {t("page.setting.mod.mod_management.title")}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col space-y-4">
            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.mod_management.deleteArchiveAfterExtract")}
                </span>
                <p className="text-sm text-muted-foreground">
                  {t("page.setting.mod.mod_management.deleteArchiveAfterExtractDescription")}
                </p>
              </div>
              <Switch
                checked={settings.deleteArchiveAfterExtract}
                onCheckedChange={(val) =>
                  update(
                    "deleteArchiveAfterExtract",
                    val,
                    "setting:mod:setDeleteArchiveAfterExtract",
                  )
                }
              />
            </div>

            <Separator />

            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.mod_management.moveFolderInsteadOfCopy")}
                </span>
                <p className="text-sm text-muted-foreground">
                  {t("page.setting.mod.mod_management.moveFolderInsteadOfCopyDescription")}
                </p>
              </div>
              <Switch
                checked={settings.moveFolderInsteadOfCopy}
                onCheckedChange={(val) =>
                  update("moveFolderInsteadOfCopy", val, "setting:mod:setMoveFolderInsteadOfCopy")
                }
              />
            </div>

            <Separator />

            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.mod_management.searchModPreview")}
                </span>
                <p className="text-sm text-muted-foreground">
                  {t("page.setting.mod.mod_management.searchModPreviewDescription")}
                </p>
              </div>
              <Switch
                checked={settings.searchModPreview}
                onCheckedChange={(val) =>
                  update("searchModPreview", val, "setting:mod:setSearchModPreview")
                }
              />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">
              {t("page.setting.mod.performance.title")}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col space-y-2" ref={anim1}>
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <span className="text-sm font-bold">
                    {t("page.setting.mod.performance.virtualization.title")}
                  </span>
                  <p className="text-xs text-muted-foreground">
                    {t("page.setting.mod.performance.virtualization.description")}
                  </p>
                </div>
                <Switch
                  checked={settings.virtualizationEnabled}
                  onCheckedChange={handleVirtualizationEnabledChange}
                />
              </div>

              {settings.virtualizationEnabled && (
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <span className="text-sm font-bold">
                      {t("page.setting.mod.performance.virtualization.threshold")}
                    </span>
                    <p className="text-xs text-muted-foreground">
                      {t("page.setting.mod.performance.virtualization.thresholdDescription")}
                    </p>
                  </div>

                  <Input
                    value={settings.virtualizationThreshold}
                    onChange={(e) =>
                      setSettings((prev) => ({
                        ...prev,
                        virtualizationThreshold: Number(e.target.value),
                      }))
                    }
                    onBlur={(e) => handleVirtualizationThresholdChange(Number(e.target.value))}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.currentTarget.blur();
                      }
                    }}
                    className="w-20"
                    disabled={!settings.virtualizationEnabled}
                  />
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
