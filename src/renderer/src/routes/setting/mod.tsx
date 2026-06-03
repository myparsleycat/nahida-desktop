import { useAutoAnimate } from "@formkit/auto-animate/react";
import { clampModGridColumnCount, clampModGridWidth } from "@renderer/components/mod/grid-layout";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { FieldDescription, FieldGroup, FieldTitle } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
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
import { Logger } from "@renderer/lib/logger";
import {
  MOD_GRID_LAYOUT_MODES,
  type ArchiveExtractPathMode,
  type ModGridLayoutMode,
  SIDEBAR_LAYOUT_MODES,
  type SidebarLayoutMode,
} from "@shared/mod";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/mod")({
  component: RouteComponent,
});

const settingsConfig = {
  archiveExtractPathMode: "mod.archiveExtractPathMode",
  deleteArchiveAfterExtract: "mod.deleteArchiveAfterExtract",
  moveFolderInsteadOfCopy: "mod.moveFolderInsteadOfCopy",
  virtualizationEnabled: "mod.virtualizationEnabled",
  virtualizationThreshold: "mod.virtualizationThreshold",
  searchModPreview: "mod.searchModPreview",
  copyShaderFixesOnEnable: "mod.copyShaderFixesOnEnable",
  sidebarLayout: "mod.sidebarLayout",
  gridLayoutMode: "mod.gridLayoutMode",
  gridResponsiveBaseWidth: "mod.gridResponsiveBaseWidth",
  gridFixedCardWidth: "mod.gridFixedCardWidth",
  gridFixedColumnCount: "mod.gridFixedColumnCount",
} as const;

function RouteComponent() {
  return <ModSettingsRouteContent />;
}

function ModSettingsRouteContent() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [anim1] = useAutoAnimate({ duration: 150 });

  const { settings, update, setSettings, isLoading } = useSettings(settingsConfig);

  if (isLoading) {
    return null;
  }

  const handleVirtualizationEnabledChange = async (checked: boolean) => {
    try {
      await update("virtualizationEnabled", checked);
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
      await update("virtualizationThreshold", value);
      toast.success("설정이 저장되었습니다.");
      queryClient.invalidateQueries({ queryKey: ["settings", "mod", "virtualization"] });
    } catch (error) {
      Logger.error(error, "ModSettings:handleVirtualizationThresholdChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  const handleGridLayoutModeChange = async (mode: ModGridLayoutMode) => {
    if (!MOD_GRID_LAYOUT_MODES.includes(mode)) {
      return;
    }

    try {
      await update("gridLayoutMode", mode);
    } catch (error) {
      Logger.error(error, "ModSettings:handleGridLayoutModeChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  const handleSidebarLayoutChange = async (mode: SidebarLayoutMode) => {
    if (!SIDEBAR_LAYOUT_MODES.includes(mode)) {
      return;
    }

    try {
      await update("sidebarLayout", mode);
    } catch (error) {
      Logger.error(error, "ModSettings:handleSidebarLayoutChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  const handleGridResponsiveBaseWidthChange = async (value: number) => {
    const nextValue = clampModGridWidth(value, 400);
    try {
      await update("gridResponsiveBaseWidth", nextValue);
      setSettings((prev) => ({ ...prev, gridResponsiveBaseWidth: nextValue }));
      toast.success("설정이 저장되었습니다.");
    } catch (error) {
      Logger.error(error, "ModSettings:handleGridResponsiveBaseWidthChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  const handleGridFixedCardWidthChange = async (value: number) => {
    const nextValue = clampModGridWidth(value, 360);
    try {
      await update("gridFixedCardWidth", nextValue);
      setSettings((prev) => ({ ...prev, gridFixedCardWidth: nextValue }));
      toast.success("설정이 저장되었습니다.");
    } catch (error) {
      Logger.error(error, "ModSettings:handleGridFixedCardWidthChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  const handleGridFixedColumnCountChange = async (value: number) => {
    const nextValue = clampModGridColumnCount(value, 4);
    try {
      await update("gridFixedColumnCount", nextValue);
      setSettings((prev) => ({ ...prev, gridFixedColumnCount: nextValue }));
      toast.success("설정이 저장되었습니다.");
    } catch (error) {
      Logger.error(error, "ModSettings:handleGridFixedColumnCountChange");
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
            <div className="flex items-center justify-between space-x-2">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.mod_management.archiveExtractPathMode")}
                </span>
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.mod.mod_management.archiveExtractPathModeDescription")}
                </p>
              </div>
              <Select
                value={settings.archiveExtractPathMode}
                onValueChange={(value: ArchiveExtractPathMode) =>
                  update("archiveExtractPathMode", value)
                }
              >
                <SelectTrigger className="w-55">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectGroup>
                    <SelectItem value="flatten_single_root">
                      {t(
                        "page.setting.mod.mod_management.archiveExtractPathModes.flatten_single_root",
                      )}
                    </SelectItem>
                    <SelectItem value="keep_archive_root">
                      {t(
                        "page.setting.mod.mod_management.archiveExtractPathModes.keep_archive_root",
                      )}
                    </SelectItem>
                    <SelectItem value="ask_every_time">
                      {t("page.setting.mod.mod_management.archiveExtractPathModes.ask_every_time")}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <Separator />

            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.mod_management.deleteArchiveAfterExtract")}
                </span>
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.mod.mod_management.deleteArchiveAfterExtractDescription")}
                </p>
              </div>
              <Switch
                checked={settings.deleteArchiveAfterExtract}
                onCheckedChange={(val) => update("deleteArchiveAfterExtract", val)}
              />
            </div>

            <Separator />

            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.mod_management.moveFolderInsteadOfCopy")}
                </span>
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.mod.mod_management.moveFolderInsteadOfCopyDescription")}
                </p>
              </div>
              <Switch
                checked={settings.moveFolderInsteadOfCopy}
                onCheckedChange={(val) => update("moveFolderInsteadOfCopy", val)}
              />
            </div>

            <Separator />

            <div className="flex items-center justify-between space-x-4">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.mod_management.copyShaderFixesOnEnable")}
                </span>
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.mod.mod_management.copyShaderFixesOnEnableDescription")}
                </p>
              </div>
              <Switch
                checked={settings.copyShaderFixesOnEnable}
                onCheckedChange={(val) => update("copyShaderFixesOnEnable", val)}
              />
            </div>

            <Separator />

            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.mod_management.searchModPreview")}
                </span>
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.mod.mod_management.searchModPreviewDescription")}
                </p>
              </div>
              <Switch
                checked={settings.searchModPreview}
                onCheckedChange={(val) => update("searchModPreview", val)}
              />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">
              {t("page.setting.mod.layout.title")}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col space-y-4">
            <div className="flex items-center justify-between space-x-4">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.layout.sidebar.mode")}
                </span>
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.mod.layout.sidebar.modeDescription")}
                </p>
              </div>
              <Select
                value={settings.sidebarLayout}
                onValueChange={(value: SidebarLayoutMode) => handleSidebarLayoutChange(value)}
              >
                <SelectTrigger className="w-55">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectGroup>
                    <SelectItem value="row">
                      {t("page.setting.mod.layout.sidebar.modes.row")}
                    </SelectItem>
                    <SelectItem value="grid">
                      {t("page.setting.mod.layout.sidebar.modes.grid")}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <Separator />

            <div className="space-y-1">
              <span className="text-sm font-medium">{t("page.setting.mod.layout.grid.mode")}</span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.mod.layout.grid.modeDescription")}
              </p>
            </div>

            <FieldGroup>
              <Select
                value={settings.gridLayoutMode}
                onValueChange={(value: ModGridLayoutMode) => handleGridLayoutModeChange(value)}
              >
                <SelectTrigger className="w-55 ml-auto">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectGroup>
                    <SelectItem value="responsive">
                      {t("page.setting.mod.layout.grid.modes.responsive")}
                    </SelectItem>
                    <SelectItem value="fixed_card_width">
                      {t("page.setting.mod.layout.grid.modes.fixed_card_width")}
                    </SelectItem>
                    <SelectItem value="fixed_column_count">
                      {t("page.setting.mod.layout.grid.modes.fixed_column_count")}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>

              {settings.gridLayoutMode === "responsive" && (
                <div className="flex items-center justify-between gap-4">
                  <div className="space-y-0.5">
                    <FieldTitle>{t("page.setting.mod.layout.grid.responsiveBaseWidth")}</FieldTitle>
                    <FieldDescription className="text-xs">
                      {t("page.setting.mod.layout.grid.responsiveBaseWidthDescription")}
                    </FieldDescription>
                  </div>
                  <Input
                    value={settings.gridResponsiveBaseWidth}
                    onChange={(e) =>
                      setSettings((prev) => ({
                        ...prev,
                        gridResponsiveBaseWidth: Number(e.target.value),
                      }))
                    }
                    onBlur={(e) => handleGridResponsiveBaseWidthChange(Number(e.target.value))}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.currentTarget.blur();
                      }
                    }}
                    className="w-24"
                    inputMode="numeric"
                  />
                </div>
              )}

              {settings.gridLayoutMode === "fixed_card_width" && (
                <div className="flex items-center justify-between gap-4">
                  <div className="space-y-0.5">
                    <FieldTitle>{t("page.setting.mod.layout.grid.fixedCardWidth")}</FieldTitle>
                    <FieldDescription className="text-xs">
                      {t("page.setting.mod.layout.grid.fixedCardWidthDescription")}
                    </FieldDescription>
                  </div>
                  <Input
                    value={settings.gridFixedCardWidth}
                    onChange={(e) =>
                      setSettings((prev) => ({
                        ...prev,
                        gridFixedCardWidth: Number(e.target.value),
                      }))
                    }
                    onBlur={(e) => handleGridFixedCardWidthChange(Number(e.target.value))}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.currentTarget.blur();
                      }
                    }}
                    className="w-24"
                    inputMode="numeric"
                  />
                </div>
              )}

              {settings.gridLayoutMode === "fixed_column_count" && (
                <div className="flex items-center justify-between gap-4">
                  <div className="space-y-0.5">
                    <FieldTitle>{t("page.setting.mod.layout.grid.fixedColumnCount")}</FieldTitle>
                    <FieldDescription className="text-xs">
                      {t("page.setting.mod.layout.grid.fixedColumnCountDescription")}
                    </FieldDescription>
                  </div>
                  <Input
                    value={settings.gridFixedColumnCount}
                    onChange={(e) =>
                      setSettings((prev) => ({
                        ...prev,
                        gridFixedColumnCount: Number(e.target.value),
                      }))
                    }
                    onBlur={(e) => handleGridFixedColumnCountChange(Number(e.target.value))}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.currentTarget.blur();
                      }
                    }}
                    className="w-24"
                    inputMode="numeric"
                  />
                </div>
              )}
            </FieldGroup>
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
