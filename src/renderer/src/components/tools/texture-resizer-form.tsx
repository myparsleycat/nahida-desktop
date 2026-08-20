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
import type { TextureColorSpace, TextureResizeSettings, TextureUpscaleModel } from "@shared/types";
import {
  getAvailableTextureUpscaleScales,
  getTextureResizeCandidates,
  isTextureUpscaleOperation,
  pickTextureResizeCandidate,
  resolveTextureUpscaleScale,
  TEXTURE_UPSCALE_MODELS,
} from "@shared/utils";
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
  sharedResize?: boolean;
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
  sharedResize = false,
  resizeSource = null,
}: TextureResizerFormProps) {
  const { t } = useTranslation();

  const updateSettings = (patch: Partial<TextureResizeSettings>) => {
    onSettingsChange({
      ...settings,
      ...patch,
    });
  };

  const colorSpaceOptions = [
    { value: "srgb", label: t("page.tools.texture_resizer.color_space.srgb") },
    { value: "linear", label: t("page.tools.texture_resizer.color_space.linear") },
  ] as const;
  const modeOptions = [
    { value: "percent", label: t("page.tools.texture_resizer.mode_options.percent") },
    { value: "custom", label: t("page.tools.texture_resizer.mode_options.custom") },
  ] as const;
  const operationOptions = [
    { value: "resize", label: t("page.tools.texture_resizer.operation_options.resize") },
    {
      value: "resize_and_convert",
      label: t("page.tools.texture_resizer.operation_options.resize_and_convert"),
    },
    { value: "convert", label: t("page.tools.texture_resizer.operation_options.convert") },
    { value: "upscale", label: t("page.tools.texture_resizer.operation_options.upscale") },
    {
      value: "upscale_and_convert",
      label: t("page.tools.texture_resizer.operation_options.upscale_and_convert"),
    },
  ] as const;
  const upscaleModelOptions = TEXTURE_UPSCALE_MODELS.map((model) => ({
    value: model,
    label: t(`page.tools.texture_resizer.upscale_model_options.${model}`),
  }));
  const availableUpscaleScales = getAvailableTextureUpscaleScales(settings.upscaleModel);
  const selectedUpscaleScale = resolveTextureUpscaleScale(
    settings.upscaleModel,
    settings.upscaleScale,
  );

  const isUpscale = isTextureUpscaleOperation(settings.operation);
  const showResizeInputs = settings.operation !== "convert" && !isUpscale;
  const showOutputFormat = settings.operation !== "resize" && settings.operation !== "upscale";
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
      <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
        {t("page.tools.texture_resizer.output_color_space")}
      </label>
      <Select
        value={selectedOutputColorSpace}
        items={colorSpaceOptions}
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
        <SelectContent>
          <SelectGroup>
            {colorSpaceOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
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
      <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
        {t("page.tools.texture_resizer.operation")}
      </label>
      <Select
        value={settings.operation}
        items={operationOptions}
        onValueChange={(value) => {
          if (value === null) return;
          const operation = operationOptions.find((option) => option.value === value)?.value;
          if (!operation) return;
          updateSettings({
            operation,
            upscaleScale: isTextureUpscaleOperation(operation)
              ? resolveTextureUpscaleScale(settings.upscaleModel, settings.upscaleScale)
              : settings.upscaleScale,
          });
        }}
      >
        <SelectTrigger disabled={disabled} className="w-56">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {operationOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
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
          <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
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

      {isUpscale ? (
        <div className="grid gap-4 md:grid-cols-2">
          {operationField}
          <div className="space-y-4">
            <div className="space-y-2">
              <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
                {t("page.tools.texture_resizer.upscale_model")}
              </label>
              <Select
                value={settings.upscaleModel}
                items={upscaleModelOptions}
                onValueChange={(value) => {
                  if (value === null) return;
                  const nextModel = value as TextureUpscaleModel;
                  updateSettings({
                    upscaleModel: nextModel,
                    upscaleScale: resolveTextureUpscaleScale(nextModel, settings.upscaleScale),
                  });
                }}
              >
                <SelectTrigger disabled={disabled} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {upscaleModelOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {t(
                  `page.tools.texture_resizer.upscale_model_descriptions.${settings.upscaleModel}`,
                )}
              </p>
            </div>
            <div className="space-y-2">
              <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
                {t("page.tools.texture_resizer.upscale_scale")}
              </label>
              <Select
                value={String(selectedUpscaleScale)}
                items={availableUpscaleScales.map((scale) => ({
                  value: String(scale),
                  label: t("page.tools.texture_resizer.upscale_scale_option", { scale }),
                }))}
                onValueChange={(value) => {
                  if (value === null) return;
                  updateSettings({
                    upscaleScale: resolveTextureUpscaleScale(
                      settings.upscaleModel,
                      Number.parseInt(value, 10),
                    ),
                  });
                }}
              >
                <SelectTrigger disabled={disabled} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {availableUpscaleScales.map((scale) => (
                      <SelectItem key={scale} value={String(scale)}>
                        {t("page.tools.texture_resizer.upscale_scale_option", { scale })}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {t("page.tools.texture_resizer.upscale_scale_description")}
              </p>
            </div>
            {currentColorSpace === "linear" && (
              <div className="rounded-md border bg-background/40 p-3 text-xs text-muted-foreground">
                {t("page.tools.texture_resizer.upscale_normal_warning")}
              </div>
            )}
          </div>
        </div>
      ) : showResizeInputs ? (
        <div className="grid gap-4 md:grid-cols-2">
          {operationField}
          {sharedResize ? (
            <div className="space-y-2">
              <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
                {t("page.tools.texture_resizer.mode")}
              </label>
              <Select
                value={settings.mode}
                items={modeOptions}
                onValueChange={(value) => {
                  if (value === null) return;
                  const nextMode = modeOptions.find((option) => option.value === value)?.value;
                  if (!nextMode) return;
                  updateSettings({ mode: nextMode });
                }}
              >
                <SelectTrigger disabled={disabled} className="w-56">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {modeOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {t(`page.tools.texture_resizer.mode_descriptions.${settings.mode}`)}
              </p>
            </div>
          ) : (
            <div className="rounded-md border bg-background/40 p-3 text-xs text-muted-foreground">
              {t("page.tools.texture_resizer.custom_hint")}
            </div>
          )}
        </div>
      ) : useHorizontalConvertLayout && showOutputFormat ? (
        <div className="grid gap-4 md:grid-cols-2">
          {operationField}
          <div className="space-y-4">
            {colorSpaceField}
            <div className="space-y-2">
              <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
                {t("page.tools.texture_resizer.output_format")}
              </label>
              <Select
                value={outputFormatValue}
                items={visibleOutputFormats.map((format) => ({
                  value: format,
                  label: formatTextureFormatLabel(format),
                }))}
                onValueChange={(value) => {
                  if (value === null) return;
                  updateSettings({ outputFormat: value });
                }}
              >
                <SelectTrigger
                  disabled={disabled || visibleOutputFormats.length === 0}
                  className="w-52"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
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

      {showResizeInputs && sharedResize && settings.mode === "percent" && (
        <div className="space-y-3 rounded-md border bg-background/40 p-3">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-sm font-medium">{t("page.tools.texture_resizer.percent")}</div>
              <div className="text-xs text-muted-foreground">
                {t("page.tools.texture_resizer.mode_descriptions.percent")}
              </div>
            </div>
            <div className="text-sm font-medium">{settings.percent}%</div>
          </div>
          <Slider
            min={1}
            max={99}
            step={1}
            value={settings.percent}
            disabled={disabled}
            onValueChange={(value) => {
              const percent = Array.isArray(value) ? (value[0] ?? 1) : value;
              updateSettings({
                percent,
                mode: "percent",
              });
            }}
          />
        </div>
      )}

      {showResizeInputs && sharedResize && settings.mode === "custom" && (
        <div className="space-y-3 rounded-md border bg-background/40 p-3">
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
                {t("page.tools.texture_resizer.custom_width")}
              </label>
              <Input
                type="number"
                min={1024}
                step={1024}
                value={settings.customWidth}
                disabled={disabled}
                onChange={(event) => {
                  const parsed = Number.parseInt(event.target.value, 10);
                  updateSettings({
                    customWidth: Number.isFinite(parsed) ? parsed : 0,
                    mode: "custom",
                  });
                }}
                onBlur={() => {
                  updateSettings({
                    customWidth: snapDimension(settings.customWidth),
                    mode: "custom",
                  });
                }}
              />
            </div>
            <div className="space-y-2">
              <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
                {t("page.tools.texture_resizer.custom_height")}
              </label>
              <Input
                type="number"
                min={1024}
                step={1024}
                value={settings.customHeight}
                disabled={disabled}
                onChange={(event) => {
                  const parsed = Number.parseInt(event.target.value, 10);
                  updateSettings({
                    customHeight: Number.isFinite(parsed) ? parsed : 0,
                    mode: "custom",
                  });
                }}
                onBlur={() => {
                  updateSettings({
                    customHeight: snapDimension(settings.customHeight),
                    mode: "custom",
                  });
                }}
              />
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            {t("page.tools.texture_resizer.custom_hint")}
          </p>
        </div>
      )}

      {showResizeInputs && !sharedResize && (
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
              {resizeCandidates.length > 1 && (
                <>
                  <Slider
                    min={0}
                    max={resizeCandidates.length - 1}
                    step={1}
                    value={selectedResizeIndex}
                    disabled={disabled}
                    onValueChange={(value) => {
                      const index = Array.isArray(value) ? (value[0] ?? 0) : value;
                      const nextCandidate = resizeCandidates[index];
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
                </>
              )}
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
            <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
              {t("page.tools.texture_resizer.output_format")}
            </label>
            <Select
              value={outputFormatValue}
              items={visibleOutputFormats.map((format) => ({
                value: format,
                label: formatTextureFormatLabel(format),
              }))}
              onValueChange={(value) => {
                if (value === null) return;
                updateSettings({ outputFormat: value });
              }}
            >
              <SelectTrigger
                disabled={disabled || visibleOutputFormats.length === 0}
                className="w-52"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
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

function snapDimension(value: number) {
  const normalized = Math.max(1024, Math.round(value));
  const remainder = normalized % 1024;
  if (remainder === 0) {
    return normalized;
  }

  return remainder < 512 ? normalized - remainder : normalized + (1024 - remainder);
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
