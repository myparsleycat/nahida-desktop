import { clampModGridColumnCount, clampModGridWidth } from "@renderer/components/mod/grid-layout";
import { ModCompressionCard } from "@renderer/components/setting/mod-compression-card";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { FieldGroup } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import {
  Section,
  SectionContent,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import { useSettings } from "@renderer/hooks/use-settings";
import { Logger } from "@renderer/lib/logger";
import {
  MOD_GRID_LAYOUT_MODES,
  DOWNLOAD_SOURCES,
  SIDEBAR_LAYOUT_MODES,
  type DisabledPrefixStyle,
  type ModGridLayoutMode,
  type SidebarLayoutMode,
} from "@shared/mod";
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
  autoInspectFix: "mod.autoInspectFix",
  searchModPreview: "mod.searchModPreview",
  autoResolveDownloadTarget: "mod.autoResolveDownloadTarget",
  autoResolveDownloadTargetSources: "mod.autoResolveDownloadTargetSources",
  copyShaderFixesOnEnable: "mod.copyShaderFixesOnEnable",
  disabledPrefixStyle: "mod.disabledPrefixStyle",
  sidebarLayout: "mod.sidebarLayout",
  gridLayoutMode: "mod.gridLayoutMode",
  gridModelPreview: "mod.gridModelPreview",
  gridResponsiveBaseWidth: "mod.gridResponsiveBaseWidth",
  gridFixedCardWidth: "mod.gridFixedCardWidth",
  gridFixedColumnCount: "mod.gridFixedColumnCount",
} as const;

const AUTO_INSPECT_FIX_LABEL_ID = "setting-mod-auto-inspect-fix-title";
const GRID_MODEL_PREVIEW_LABEL_ID = "setting-mod-grid-model-preview-title";

function RouteComponent() {
  return <ModSettingsRouteContent />;
}

function ModSettingsRouteContent() {
  const { t } = useTranslation();

  const { settings, update, setSettings, isLoading } = useSettings(settingsConfig);

  const archiveExtractPathModeOptions = [
    {
      value: "flatten_single_root",
      label: t("page.setting.mod.mod_management.archiveExtractPathModes.flatten_single_root"),
    },
    {
      value: "keep_archive_root",
      label: t("page.setting.mod.mod_management.archiveExtractPathModes.keep_archive_root"),
    },
    {
      value: "ask_every_time",
      label: t("page.setting.mod.mod_management.archiveExtractPathModes.ask_every_time"),
    },
  ] as const;

  const sidebarLayoutOptions = [
    { value: "row", label: t("page.setting.mod.layout.sidebar.modes.row") },
    { value: "grid", label: t("page.setting.mod.layout.sidebar.modes.grid") },
  ] as const;

  const gridLayoutModeOptions = [
    { value: "responsive", label: t("page.setting.mod.layout.grid.modes.responsive") },
    {
      value: "fixed_card_width",
      label: t("page.setting.mod.layout.grid.modes.fixed_card_width"),
    },
    {
      value: "fixed_column_count",
      label: t("page.setting.mod.layout.grid.modes.fixed_column_count"),
    },
  ] as const;

  const disabledPrefixStyleOptions = [
    { value: "space", label: t("page.setting.mod.mod_management.disabledPrefixStyles.space") },
    {
      value: "underscore",
      label: t("page.setting.mod.mod_management.disabledPrefixStyles.underscore"),
    },
  ] as const;

  if (isLoading) {
    return null;
  }

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

  const handleAutoInspectFixChange = async (val: boolean) => {
    const previous = settings.autoInspectFix;
    try {
      await update("autoInspectFix", val);
    } catch (error) {
      Logger.error(error, "ModSettings:handleAutoInspectFixChange");
      await update("autoInspectFix", previous).catch(() => {});
      toast.error(t("page.setting.tools.wuwaFixer.autoUpdateNotification.save_failed"));
    }
  };

  const handleGridResponsiveBaseWidthChange = async (value: number) => {
    const nextValue = clampModGridWidth(value, 400);
    try {
      await update("gridResponsiveBaseWidth", nextValue);
      setSettings((prev) => ({ ...prev, gridResponsiveBaseWidth: nextValue }));
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
    } catch (error) {
      Logger.error(error, "ModSettings:handleGridFixedColumnCountChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  return (
    <div className="space-y-6 p-4">
      <div className="space-y-6">
        <Section>
          <SectionHeader>
            <SectionTitle>{t("page.setting.mod.mod_management.title")}</SectionTitle>
          </SectionHeader>
          <SectionContent>
            <SectionRow
              title={t("page.setting.mod.mod_management.archiveExtractPathMode")}
              description={t("page.setting.mod.mod_management.archiveExtractPathModeDescription")}
            >
              <Select
                value={settings.archiveExtractPathMode}
                items={archiveExtractPathModeOptions}
                onValueChange={(value) => {
                  if (value === null) return;
                  void update("archiveExtractPathMode", value);
                }}
              >
                <SelectTrigger className="w-55">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {archiveExtractPathModeOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </SectionRow>
            <SectionRow
              title={t("page.setting.mod.mod_management.deleteArchiveAfterExtract")}
              description={t(
                "page.setting.mod.mod_management.deleteArchiveAfterExtractDescription",
              )}
            >
              <Switch
                checked={settings.deleteArchiveAfterExtract}
                onCheckedChange={(val) => update("deleteArchiveAfterExtract", val)}
              />
            </SectionRow>
            <SectionRow
              title={t("page.setting.mod.mod_management.moveFolderInsteadOfCopy")}
              description={t("page.setting.mod.mod_management.moveFolderInsteadOfCopyDescription")}
            >
              <Switch
                checked={settings.moveFolderInsteadOfCopy}
                onCheckedChange={(val) => update("moveFolderInsteadOfCopy", val)}
              />
            </SectionRow>
            <SectionRow
              titleId={AUTO_INSPECT_FIX_LABEL_ID}
              title={t("page.setting.mod.mod_management.autoInspectFix")}
              description={t("page.setting.mod.mod_management.autoInspectFixDescription")}
            >
              <Switch
                checked={settings.autoInspectFix}
                aria-labelledby={AUTO_INSPECT_FIX_LABEL_ID}
                onCheckedChange={(val) => void handleAutoInspectFixChange(val)}
              />
            </SectionRow>
            <SectionRow
              title={t("page.setting.mod.mod_management.copyShaderFixesOnEnable")}
              description={t("page.setting.mod.mod_management.copyShaderFixesOnEnableDescription")}
            >
              <Switch
                checked={settings.copyShaderFixesOnEnable}
                onCheckedChange={(val) => update("copyShaderFixesOnEnable", val)}
              />
            </SectionRow>
            <SectionRow
              title={t("page.setting.mod.mod_management.disabledPrefixStyle")}
              description={t("page.setting.mod.mod_management.disabledPrefixStyleDescription")}
            >
              <Select
                value={settings.disabledPrefixStyle}
                items={disabledPrefixStyleOptions}
                onValueChange={(val) => update("disabledPrefixStyle", val as DisabledPrefixStyle)}
              >
                <SelectTrigger className="w-[180px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {disabledPrefixStyleOptions.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </SectionRow>
            <SectionRow
              title={t("page.setting.mod.mod_management.searchModPreview")}
              description={t("page.setting.mod.mod_management.searchModPreviewDescription")}
            >
              <Switch
                checked={settings.searchModPreview}
                onCheckedChange={(val) => update("searchModPreview", val)}
              />
            </SectionRow>
            <div className="space-y-3">
              <SectionRow
                title={t("page.setting.mod.mod_management.autoResolveDownloadTarget")}
                description={t(
                  "page.setting.mod.mod_management.autoResolveDownloadTargetDescription",
                )}
              >
                <Switch
                  checked={settings.autoResolveDownloadTarget}
                  onCheckedChange={(val) => update("autoResolveDownloadTarget", val)}
                />
              </SectionRow>

              <div className="grid gap-2 rounded-md bg-muted/50 p-3 sm:grid-cols-3">
                {DOWNLOAD_SOURCES.map((source) => (
                  <label
                    key={source}
                    className="flex cursor-pointer items-center gap-2 text-sm has-disabled:cursor-not-allowed has-disabled:opacity-50"
                  >
                    <Checkbox
                      checked={settings.autoResolveDownloadTargetSources.includes(source)}
                      disabled={!settings.autoResolveDownloadTarget}
                      onCheckedChange={(checked) =>
                        update("autoResolveDownloadTargetSources", (currentSources) =>
                          checked
                            ? currentSources.includes(source)
                              ? currentSources
                              : [...currentSources, source]
                            : currentSources.filter((selectedSource) => selectedSource !== source),
                        )
                      }
                    />
                    {t(`page.setting.mod.mod_management.downloadSources.${source}`)}
                  </label>
                ))}
              </div>
            </div>
          </SectionContent>
        </Section>

        <Section>
          <SectionHeader>
            <SectionTitle>{t("page.setting.mod.layout.title")}</SectionTitle>
          </SectionHeader>
          <SectionContent>
            <SectionRow
              title={t("page.setting.mod.layout.sidebar.mode")}
              description={t("page.setting.mod.layout.sidebar.modeDescription")}
            >
              <Select
                value={settings.sidebarLayout}
                items={sidebarLayoutOptions}
                onValueChange={(value) => {
                  if (value === null) return;
                  void handleSidebarLayoutChange(value);
                }}
              >
                <SelectTrigger className="w-55">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {sidebarLayoutOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </SectionRow>
            <SectionRow
              titleId={GRID_MODEL_PREVIEW_LABEL_ID}
              title={t("page.setting.mod.layout.gridModelPreview")}
              description={t("page.setting.mod.layout.gridModelPreviewDescription")}
            >
              <Switch
                checked={settings.gridModelPreview}
                aria-labelledby={GRID_MODEL_PREVIEW_LABEL_ID}
                onCheckedChange={(value) => update("gridModelPreview", value)}
              />
            </SectionRow>
            <div className="space-y-1">
              <span className="text-sm font-medium">{t("page.setting.mod.layout.grid.mode")}</span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.mod.layout.grid.modeDescription")}
              </p>
            </div>

            <FieldGroup>
              <Select
                value={settings.gridLayoutMode}
                items={gridLayoutModeOptions}
                onValueChange={(value) => {
                  if (value === null) return;
                  void handleGridLayoutModeChange(value);
                }}
              >
                <SelectTrigger className="ml-auto w-55">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {gridLayoutModeOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>

              {settings.gridLayoutMode === "responsive" && (
                <SectionRow
                  title={t("page.setting.mod.layout.grid.responsiveBaseWidth")}
                  description={t("page.setting.mod.layout.grid.responsiveBaseWidthDescription")}
                >
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
                </SectionRow>
              )}

              {settings.gridLayoutMode === "fixed_card_width" && (
                <SectionRow
                  title={t("page.setting.mod.layout.grid.fixedCardWidth")}
                  description={t("page.setting.mod.layout.grid.fixedCardWidthDescription")}
                >
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
                </SectionRow>
              )}

              {settings.gridLayoutMode === "fixed_column_count" && (
                <SectionRow
                  title={t("page.setting.mod.layout.grid.fixedColumnCount")}
                  description={t("page.setting.mod.layout.grid.fixedColumnCountDescription")}
                >
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
                </SectionRow>
              )}
            </FieldGroup>
          </SectionContent>
        </Section>

        <ModCompressionCard />
      </div>
    </div>
  );
}
