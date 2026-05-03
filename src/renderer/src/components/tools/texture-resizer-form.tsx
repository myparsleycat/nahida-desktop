import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Slider } from "@renderer/components/ui/slider";
import { Switch } from "@renderer/components/ui/switch";
import type { TextureColorSpace, TextureResizeSettings } from "@shared/types";
import { getTextureResizeCandidates, pickTextureResizeCandidate } from "@shared/utils";
import { FolderOpenIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

interface TextureResizerFormProps {
  settings: TextureResizeSettings;
  onSettingsChange: (settings: TextureResizeSettings) => void;
  disabled?: boolean;
  targetPath?: string;
  onTargetPathChange?: (value: string) => void;
  onBrowseTargetPath?: () => void;
  showTargetPath?: boolean;
  availableOutputFormats?: string[];
  currentFormat?: string;
  currentColorSpace?: TextureColorSpace;
  formatConversionMessage?: string | null;
  resizeSource?: {
    width: number;
    height: number;
  } | null;
}

export function TextureResizerForm({
  settings,
  onSettingsChange,
  disabled = false,
  targetPath = "",
  onTargetPathChange,
  onBrowseTargetPath,
  showTargetPath = true,
  availableOutputFormats = [],
  currentFormat,
  currentColorSpace = "unknown",
  formatConversionMessage,
  resizeSource = null,
}: TextureResizerFormProps) {
  const { t } = useTranslation();

  const updateSettings = (patch: Partial<TextureResizeSettings>) => {
    onSettingsChange({
      ...settings,
      ...patch,
    });
  };

  const showResizeInputs = settings.operation !== "convert";
  const showOutputFormat = settings.operation !== "resize";
  const useHorizontalConvertLayout = settings.operation === "convert";
  const resizeCandidates = resizeSource
    ? getTextureResizeCandidates(resizeSource.width, resizeSource.height)
    : [];
  const selectedResizeCandidate =
    resizeCandidates.length > 0
      ? (pickTextureResizeCandidate(
          resizeCandidates,
          settings.customWidth,
          settings.customHeight,
        ) ?? resizeCandidates[0])
      : null;
  const selectedResizeIndex =
    selectedResizeCandidate != null
      ? resizeCandidates.findIndex(
          (candidate) =>
            candidate.width === selectedResizeCandidate.width &&
            candidate.height === selectedResizeCandidate.height,
        )
      : -1;
  const colorSpaceSelectionEnabled =
    showOutputFormat && currentColorSpace === "unknown" && availableOutputFormats.length > 0;
  const selectedOutputColorSpace = inferOutputColorSpace(
    settings.outputFormat || currentFormat || availableOutputFormats[0] || "",
  );
  const visibleOutputFormats = colorSpaceSelectionEnabled
    ? availableOutputFormats.filter((format) =>
        selectedOutputColorSpace === "srgb" ? format.endsWith("_SRGB") : !format.endsWith("_SRGB"),
      )
    : availableOutputFormats;
  const outputFormatValue =
    visibleOutputFormats.includes(settings.outputFormat) && settings.outputFormat
      ? settings.outputFormat
      : currentFormat && visibleOutputFormats.includes(currentFormat)
        ? currentFormat
        : (visibleOutputFormats[0] ?? "");

  const colorSpaceField = colorSpaceSelectionEnabled ? (
    <div className="space-y-2">
      <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
        {t("page.tools.texture_resizer.output_color_space")}
      </label>
      <Select
        value={selectedOutputColorSpace}
        onValueChange={(value) => {
          const nextColorSpace = value as Exclude<TextureColorSpace, "unknown">;
          const nextOutputFormat = availableOutputFormats.find((format) =>
            nextColorSpace === "srgb" ? format.endsWith("_SRGB") : !format.endsWith("_SRGB"),
          );

          updateSettings({
            outputFormat: nextOutputFormat ?? "",
          });
        }}
      >
        <SelectTrigger disabled={disabled}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent position="popper">
          <SelectGroup>
            <SelectItem value="srgb">{t("page.tools.texture_resizer.color_space.srgb")}</SelectItem>
            <SelectItem value="linear">
              {t("page.tools.texture_resizer.color_space.linear")}
            </SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
      <p className="text-xs text-muted-foreground">
        {t("page.tools.texture_resizer.output_color_space_description")}
      </p>
    </div>
  ) : null;

  const operationField = (
    <div className="space-y-2">
      <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
        {t("page.tools.texture_resizer.operation")}
      </label>
      <Select
        value={settings.operation}
        onValueChange={(value) =>
          updateSettings({ operation: value as TextureResizeSettings["operation"] })
        }
      >
        <SelectTrigger disabled={disabled}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent position="popper">
          <SelectGroup>
            <SelectItem value="resize">
              {t("page.tools.texture_resizer.operation_options.resize")}
            </SelectItem>
            <SelectItem value="resize_and_convert">
              {t("page.tools.texture_resizer.operation_options.resize_and_convert")}
            </SelectItem>
            <SelectItem value="convert">
              {t("page.tools.texture_resizer.operation_options.convert")}
            </SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
      <p className="text-xs text-muted-foreground">
        {t(`page.tools.texture_resizer.operation_descriptions.${settings.operation}`)}
      </p>
    </div>
  );

  return (
    <div className="grid gap-4 rounded-lg border bg-card p-4">
      {showTargetPath && (
        <div className="space-y-2">
          <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
            {t("page.tools.texture_resizer.target_folder")}
          </label>
          <div className="flex gap-2">
            <Input
              value={targetPath}
              onChange={(event) => onTargetPathChange?.(event.target.value)}
              placeholder={t("page.tools.texture_resizer.target_folder_placeholder")}
              disabled={disabled}
            />
            <Button
              type="button"
              variant="outline"
              className="shrink-0 gap-1"
              onClick={onBrowseTargetPath}
              disabled={disabled}
            >
              <FolderOpenIcon className="size-4" />
              {t("page.tools.texture_resizer.browse")}
            </Button>
          </div>
        </div>
      )}

      {showResizeInputs ? (
        <div className="grid gap-4 md:grid-cols-2">
          {operationField}
          <div className="rounded-md border bg-background/40 p-3 text-xs text-muted-foreground">
            {t("page.tools.texture_resizer.custom_hint")}
          </div>
        </div>
      ) : useHorizontalConvertLayout && showOutputFormat ? (
        <div className="grid gap-4 md:grid-cols-2">
          {operationField}
          <div className="space-y-4">
            {colorSpaceField}
            <div className="space-y-2">
              <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                {t("page.tools.texture_resizer.output_format")}
              </label>
              <Select
                value={outputFormatValue}
                onValueChange={(value) => updateSettings({ outputFormat: value })}
              >
                <SelectTrigger disabled={disabled || visibleOutputFormats.length === 0}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectGroup>
                    {visibleOutputFormats.map((format) => (
                      <SelectItem key={format} value={format}>
                        {formatTextureFormatLabel(format)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {formatConversionMessage ??
                  t("page.tools.texture_resizer.output_format_description")}
              </p>
            </div>
          </div>
        </div>
      ) : (
        operationField
      )}

      {showResizeInputs && (
        <>
          {resizeSource && resizeCandidates.length > 0 && selectedResizeCandidate ? (
            <div className="space-y-3 rounded-md border bg-background/40 p-3">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <div className="text-sm font-medium">
                    {t("page.tools.texture_resizer.resize_step")}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {t("page.tools.texture_resizer.resize_step_description")}
                  </div>
                </div>
                <div className="text-right">
                  <div className="text-sm font-medium">
                    {formatResizePercent(selectedResizeCandidate.width, resizeSource.width)}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {selectedResizeCandidate.width}x{selectedResizeCandidate.height}
                  </div>
                </div>
              </div>
              <Slider
                min={0}
                max={resizeCandidates.length - 1}
                step={1}
                value={[selectedResizeIndex]}
                disabled={disabled}
                onValueChange={(value) => {
                  const nextCandidate = resizeCandidates[value[0] ?? 0];
                  if (!nextCandidate) {
                    return;
                  }

                  updateSettings({
                    customWidth: nextCandidate.width,
                    customHeight: nextCandidate.height,
                    mode: "custom",
                  });
                }}
              />
              <div className="flex justify-between gap-3 text-xs text-muted-foreground">
                <span>
                  {formatResizePercent(resizeCandidates[0].width, resizeSource.width)} /{" "}
                  {resizeCandidates[0].width}x{resizeCandidates[0].height}
                </span>
                <span className="text-right">
                  {formatResizePercent(
                    resizeCandidates[resizeCandidates.length - 1].width,
                    resizeSource.width,
                  )}{" "}
                  / {resizeCandidates[resizeCandidates.length - 1].width}x
                  {resizeCandidates[resizeCandidates.length - 1].height}
                </span>
              </div>
            </div>
          ) : (
            <div className="rounded-md border bg-background/40 p-3 text-xs text-muted-foreground">
              {t("page.tools.texture_resizer.resize_step_unavailable")}
            </div>
          )}
        </>
      )}

      {showOutputFormat && !useHorizontalConvertLayout && (
        <div className={colorSpaceField ? "grid gap-4 md:grid-cols-2" : "space-y-2"}>
          {colorSpaceField}
          <div className="space-y-2">
            <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
              {t("page.tools.texture_resizer.output_format")}
            </label>
            <Select
              value={outputFormatValue}
              onValueChange={(value) => updateSettings({ outputFormat: value })}
            >
              <SelectTrigger disabled={disabled || visibleOutputFormats.length === 0}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent position="popper">
                <SelectGroup>
                  {visibleOutputFormats.map((format) => (
                    <SelectItem key={format} value={format}>
                      {formatTextureFormatLabel(format)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {formatConversionMessage ?? t("page.tools.texture_resizer.output_format_description")}
            </p>
          </div>
        </div>
      )}

      <div className="flex items-center justify-between rounded-md border bg-background/40 p-3">
        <div>
          <div className="text-sm font-medium">{t("page.tools.texture_resizer.backup")}</div>
          <div className="text-xs text-muted-foreground">
            {t("page.tools.texture_resizer.backup_description")}
          </div>
        </div>
        <Switch
          checked={settings.backup}
          onCheckedChange={(checked) => updateSettings({ backup: checked })}
          disabled={disabled}
        />
      </div>
    </div>
  );
}

function formatResizePercent(width: number, originalWidth: number): string {
  const percent = (width / originalWidth) * 100;
  const rounded =
    Math.abs(percent - Math.round(percent)) < 0.05
      ? Math.round(percent).toString()
      : percent.toFixed(1);
  return `${rounded}%`;
}

function inferOutputColorSpace(value: string): Exclude<TextureColorSpace, "unknown"> {
  return value.endsWith("_SRGB") ? "srgb" : "linear";
}

export function formatTextureFormatLabel(format: string): string {
  return format.replace(/^DXGI_FORMAT_/, "").replaceAll("_", " ");
}
