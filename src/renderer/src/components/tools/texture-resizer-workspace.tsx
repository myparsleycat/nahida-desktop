// oxlint-disable react/no-children-prop
import {
  TextureResizerForm,
  formatTextureFormatLabel,
} from "@renderer/components/tools/texture-resizer-form";
import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Progress } from "@renderer/components/ui/progress";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Switch } from "@renderer/components/ui/switch";
import { cn } from "@renderer/lib/utils";
import type {
  TextureColorSpace,
  TextureResizeFileResult,
  TextureResizeListItem,
  TextureResizeSettings,
  TextureUpscaleProgressEvent,
} from "@shared/types";
import {
  formatSize,
  getTextureResizeCandidates,
  getTextureUpscaleTarget,
  isTextureUpscaleOperation,
  isUnsupportedTextureUpscaleFormat,
  pickTextureResizeCandidate,
  toErrorMessage,
} from "@shared/utils";
import { useForm } from "@tanstack/react-form";
import { FolderOpenIcon, ImageIcon, Loader2Icon, RefreshCwIcon } from "lucide-react";
import { Fragment, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface TextureResizerWorkspaceProps {
  mode: "folder" | "mod";
  modName?: string;
  fixedTargetPath?: string;
}

const DEFAULT_SETTINGS: TextureResizeSettings = {
  mode: "custom",
  operation: "resize",
  percent: 50,
  customWidth: 2048,
  customHeight: 2048,
  outputFormat: "",
  backup: true,
  upscaleScale: 2,
  upscaleModel: "realesr-animevideov3",
};

export function TextureResizerWorkspace({
  mode,
  modName,
  fixedTargetPath,
}: TextureResizerWorkspaceProps) {
  const { t } = useTranslation();
  const [targetPath, setTargetPath] = useState(fixedTargetPath ?? "");
  const [textures, setTextures] = useState<TextureResizeListItem[]>([]);
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [isListing, setIsListing] = useState(false);
  const [runningFilePath, setRunningFilePath] = useState<string | null>(null);
  const [batchProgress, setBatchProgress] = useState<{ current: number; total: number } | null>(
    null,
  );
  const [loadedTargetPath, setLoadedTargetPath] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [bulkApply, setBulkApply] = useState(true);
  const [activeDialogTexturePath, setActiveDialogTexturePath] = useState<string | null>(null);
  const [perTextureSettings, setPerTextureSettings] = useState<
    Record<string, TextureResizeSettings>
  >({});
  const [upscaleProgress, setUpscaleProgress] = useState<TextureUpscaleProgressEvent | null>(null);
  const settingsForm = useForm({
    defaultValues: DEFAULT_SETTINGS,
    onSubmit: async () => {},
  });
  const dialogSettingsForm = useForm({
    defaultValues: DEFAULT_SETTINGS,
    onSubmit: async () => {},
  });

  const isBusy = isListing || runningFilePath !== null || batchProgress !== null;
  const processableTextures = textures.filter((texture) => texture.canProcess);
  const dialogTextures = textures.filter((texture) => selectedPaths.has(texture.filePath));
  const allProcessableSelected =
    processableTextures.length > 0 &&
    processableTextures.every((texture) => selectedPaths.has(texture.filePath));
  const someProcessableSelected = processableTextures.some((texture) =>
    selectedPaths.has(texture.filePath),
  );

  useEffect(() => {
    setTargetPath(fixedTargetPath ?? "");
  }, [fixedTargetPath]);

  useEffect(() => {
    return window.api.on("tools:textureUpscaleProgress", (event) => {
      setUpscaleProgress(event);
    });
  }, []);

  useEffect(() => {
    window.api
      .invoke("tools:getTextureResizeSettings")
      .then((nextSettings) => {
        settingsForm.reset(nextSettings);
        if (mode === "mod" && fixedTargetPath) {
          void loadTextures(fixedTargetPath, nextSettings);
        }
      })
      .catch((error) => {
        toast.error(t("page.tools.texture_resizer.toast.load_failed"), {
          description: toErrorMessage(error),
        });
      });
  }, [fixedTargetPath, mode, settingsForm, t]);

  const browseTargetPath = async () => {
    const selected = await window.api.invoke("util:showOpenDialog", {
      properties: ["openDirectory"],
    });
    const filePath = selected.filePaths[0];
    if (filePath) {
      setTargetPath(filePath);
      await loadTextures(filePath, settingsForm.state.values);
    }
  };

  const loadTextures = async (
    nextTargetPath = targetPath,
    nextSettings = settingsForm.state.values,
  ) => {
    const normalizedTargetPath = nextTargetPath.trim();
    if (!normalizedTargetPath) {
      return;
    }

    setIsListing(true);
    try {
      const nextTextures =
        mode === "mod"
          ? await window.api.invoke("tools:listTextureMod", normalizedTargetPath, nextSettings)
          : await window.api.invoke("tools:listTextureFolder", normalizedTargetPath, nextSettings);
      setTextures(nextTextures);
      setLoadedTargetPath(normalizedTargetPath);
      const available = new Set(
        nextTextures.filter((texture) => texture.canProcess).map((texture) => texture.filePath),
      );
      setSelectedPaths(
        (current) => new Set([...current].filter((filePath) => available.has(filePath))),
      );
    } catch (error) {
      toast.error(t("page.tools.texture_resizer.toast.load_failed"), {
        description: toErrorMessage(error),
      });
    } finally {
      setIsListing(false);
    }
  };

  const toggleTexture = (filePath: string, selected: boolean) => {
    setSelectedPaths((current) => {
      const next = new Set(current);
      if (selected) {
        next.add(filePath);
        return next;
      }
      next.delete(filePath);
      return next;
    });
  };

  const toggleAllProcessable = (selected: boolean) => {
    if (selected) {
      setSelectedPaths(new Set(processableTextures.map((texture) => texture.filePath)));
      return;
    }
    setSelectedPaths(new Set());
  };

  const openProcessDialog = () => {
    const selected = textures.filter((texture) => selectedPaths.has(texture.filePath));
    if (selected.length === 0) {
      return;
    }

    const shared = settingsForm.state.values;
    const useBulk = selected.length > 1;
    setBulkApply(useBulk);
    setActiveDialogTexturePath(selected[0].filePath);
    setPerTextureSettings(
      Object.fromEntries(
        selected.map((texture) => [texture.filePath, buildDialogSettings(shared, texture)]),
      ),
    );
    dialogSettingsForm.reset(
      useBulk
        ? buildSharedDialogSettings(shared, selected)
        : buildDialogSettings(shared, selected[0]),
    );
    setDialogOpen(true);
  };

  const selectDialogTexture = (filePath: string) => {
    if (filePath === activeDialogTexturePath) {
      return;
    }

    const flushed = activeDialogTexturePath
      ? {
          ...perTextureSettings,
          [activeDialogTexturePath]: dialogSettingsForm.state.values,
        }
      : perTextureSettings;
    setPerTextureSettings(flushed);
    const nextSettings = flushed[filePath];
    if (nextSettings) {
      dialogSettingsForm.reset(nextSettings);
    }
    setActiveDialogTexturePath(filePath);
  };

  const handleBulkApplyChange = (checked: boolean) => {
    if (checked) {
      setBulkApply(true);
      return;
    }

    const shared = dialogSettingsForm.state.values;
    const nextPerTexture = Object.fromEntries(
      dialogTextures.map((texture) => [texture.filePath, buildDialogSettings(shared, texture)]),
    );
    setPerTextureSettings(nextPerTexture);
    const first = dialogTextures[0];
    if (first) {
      dialogSettingsForm.reset(nextPerTexture[first.filePath]);
      setActiveDialogTexturePath(first.filePath);
    }
    setBulkApply(false);
  };

  const closeDialog = () => {
    if (batchProgress != null) {
      return;
    }
    setDialogOpen(false);
  };

  const processSelectedTextures = async (dialogSettings: TextureResizeSettings) => {
    if (batchProgress != null) {
      return;
    }

    const selected = textures.filter((texture) => selectedPaths.has(texture.filePath));
    if (selected.length === 0) {
      return;
    }

    const flushedPerTexture = activeDialogTexturePath
      ? {
          ...perTextureSettings,
          [activeDialogTexturePath]: dialogSettings,
        }
      : perTextureSettings;

    setBatchProgress({ current: 0, total: selected.length });
    setUpscaleProgress(null);

    let updated = 0;
    let failed = 0;
    let skipped = 0;
    let lastSettings = dialogSettings;
    let lastFileResult: TextureResizeFileResult | undefined;

    for (const [index, texture] of selected.entries()) {
      setBatchProgress({ current: index + 1, total: selected.length });
      const nextSettings =
        bulkApply || selected.length === 1
          ? adaptSettingsForTexture(dialogSettings, texture)
          : (flushedPerTexture[texture.filePath] ?? dialogSettings);
      lastSettings = nextSettings;

      if (!canRun(nextSettings, texture)) {
        skipped += 1;
        continue;
      }

      setRunningFilePath(texture.filePath);
      setUpscaleProgress(null);
      settingsForm.reset(nextSettings);
      try {
        const nextResult = await window.api.invoke("tools:resizeTextureFile", {
          filePath: texture.filePath,
          settings: nextSettings,
        });
        const fileResult = nextResult.files[0];
        lastFileResult = fileResult;
        if (fileResult?.status === "updated") {
          updated += 1;
          continue;
        }
        if (fileResult?.status === "failed" || !fileResult) {
          failed += 1;
          continue;
        }
        skipped += 1;
      } catch {
        failed += 1;
      }
    }

    setRunningFilePath(null);
    setUpscaleProgress(null);
    setBatchProgress(null);
    setDialogOpen(false);
    setSelectedPaths(new Set());

    showTextureResizeResultToast(t, {
      selectedCount: selected.length,
      updated,
      failed,
      skipped,
      lastFileResult,
    });

    if (loadedTargetPath) {
      await loadTextures(loadedTargetPath, lastSettings);
    }
  };

  const hasTarget = targetPath.trim().length > 0;

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      {mode === "folder" && (
        <div className="flex gap-2">
          <Input
            value={targetPath}
            onChange={(event) => setTargetPath(event.target.value)}
            placeholder={t("page.tools.texture_resizer.target_folder_placeholder")}
            disabled={isBusy}
          />
          <Button
            type="button"
            variant="outline"
            className="shrink-0 gap-1"
            onClick={() => void browseTargetPath()}
            disabled={isBusy}
          >
            <FolderOpenIcon className="size-4" />
            {t("page.tools.texture_resizer.browse")}
          </Button>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {mode === "folder" && (
          <Button
            onClick={() => void loadTextures()}
            disabled={!hasTarget || isBusy}
            className="gap-2"
          >
            {isListing ? (
              <Loader2Icon className="size-4 animate-spin" />
            ) : (
              <FolderOpenIcon className="size-4" />
            )}
            {t("page.tools.texture_resizer.load")}
          </Button>
        )}
        {(mode === "mod" || loadedTargetPath) && (
          <Button
            variant="outline"
            onClick={() =>
              void loadTextures(mode === "mod" ? (fixedTargetPath ?? targetPath) : loadedTargetPath)
            }
            disabled={isBusy || (!loadedTargetPath && mode !== "mod")}
            className="gap-2"
          >
            {isListing ? (
              <Loader2Icon className="size-4 animate-spin" />
            ) : (
              <RefreshCwIcon className="size-4" />
            )}
            {t("page.tools.texture_resizer.refresh")}
          </Button>
        )}
        {textures.length > 0 && (
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <span className="text-xs text-muted-foreground">
              {t("page.tools.texture_resizer.selected_count", { count: selectedPaths.size })}
            </span>
            <Button
              className="gap-2"
              disabled={selectedPaths.size === 0 || isBusy}
              onClick={openProcessDialog}
            >
              <ImageIcon className="size-4" />
              {t("page.tools.texture_resizer.process_selected")}
            </Button>
          </div>
        )}
      </div>

      <div className="flex min-h-0 flex-1 flex-col rounded-lg border border-current/15 bg-card/50">
        <div className="border-b border-current/15 px-4 py-3">
          <div className="font-medium">{t("page.tools.texture_resizer.texture_list_title")}</div>
          <div className="text-xs text-muted-foreground">
            {modName ||
              loadedTargetPath ||
              fixedTargetPath ||
              t("page.tools.texture_resizer.texture_list_empty")}
          </div>
        </div>

        <ScrollArea className="min-h-0 flex-1">
          <div className="relative w-full">
            {textures.length > 0 ? (
              <table className="w-full table-auto border-collapse text-sm">
                <thead>
                  <tr>
                    <td colSpan={4} />
                    <td className="w-[1%] p-2 pr-6 text-right align-middle">
                      <div className="flex justify-end">
                        <Checkbox
                          checked={allProcessableSelected}
                          indeterminate={someProcessableSelected && !allProcessableSelected}
                          disabled={isBusy || processableTextures.length === 0}
                          aria-label={t("page.tools.texture_resizer.select_all")}
                          onCheckedChange={(checked) => toggleAllProcessable(checked === true)}
                        />
                      </div>
                    </td>
                  </tr>
                </thead>
                <tbody>
                  {textures.map((texture, index) => (
                    <Fragment key={texture.filePath}>
                      <TextureItemRow
                        texture={texture}
                        t={t}
                        selected={selectedPaths.has(texture.filePath)}
                        isRunning={runningFilePath === texture.filePath}
                        disabled={isBusy}
                        onToggle={(selected) => toggleTexture(texture.filePath, selected)}
                      />
                      {index < textures.length - 1 && (
                        <tr aria-hidden="true">
                          <td colSpan={5} className="px-3 py-0">
                            <div className="border-b border-current/15" />
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  ))}
                </tbody>
              </table>
            ) : (
              <div className="m-4 rounded-md border border-dashed bg-background/40 px-4 py-10 text-center text-sm text-muted-foreground">
                {isListing
                  ? t("page.tools.texture_resizer.loading_textures")
                  : t("page.tools.texture_resizer.texture_list_empty")}
              </div>
            )}
          </div>
        </ScrollArea>
      </div>

      <Dialog
        open={dialogOpen}
        onOpenChange={(open) => {
          if (!open) {
            closeDialog();
          }
        }}
      >
        <DialogContent className="grid max-h-[80vh] min-w-xl grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden">
          <DialogHeader>
            <DialogTitle>{t("page.tools.texture_resizer.dialog.title")}</DialogTitle>
            <DialogDescription>
              {dialogTextures.length > 1 && bulkApply
                ? t("page.tools.texture_resizer.dialog.description_multiple", {
                    count: dialogTextures.length,
                  })
                : (dialogTextures.find((texture) => texture.filePath === activeDialogTexturePath)
                    ?.fileName ?? t("page.tools.texture_resizer.dialog.description_fallback"))}
            </DialogDescription>
          </DialogHeader>

          <ScrollArea className="min-h-0">
            <dialogSettingsForm.Subscribe
              selector={(state) => state.values}
              children={(dialogSettings) => {
                const activeTexture =
                  dialogTextures.find((texture) => texture.filePath === activeDialogTexturePath) ??
                  dialogTextures[0] ??
                  null;
                const formTexture = bulkApply ? null : activeTexture;
                const selectedTexturePreview =
                  formTexture != null ? resolveTexturePreview(formTexture, dialogSettings) : null;
                const sharedFormats = intersectOutputFormats(dialogTextures);
                const sharedColor = sharedColorSpace(dialogTextures);

                return (
                  <div className="space-y-3 pr-4">
                    {dialogTextures.length > 1 && (
                      <div className="flex items-center justify-between rounded-md border bg-background/40 p-3">
                        <div>
                          <div className="text-sm font-medium">
                            {t("page.tools.texture_resizer.bulk_apply")}
                          </div>
                          <div className="text-xs text-muted-foreground">
                            {t("page.tools.texture_resizer.bulk_apply_description")}
                          </div>
                        </div>
                        <Switch
                          checked={bulkApply}
                          onCheckedChange={handleBulkApplyChange}
                          disabled={batchProgress != null}
                        />
                      </div>
                    )}
                    {!bulkApply && dialogTextures.length > 1 && (
                      <div className="flex flex-wrap gap-1">
                        {dialogTextures.map((texture) => (
                          <Button
                            key={texture.filePath}
                            type="button"
                            size="sm"
                            variant={
                              texture.filePath === activeDialogTexturePath ? "default" : "outline"
                            }
                            disabled={batchProgress != null}
                            onClick={() => selectDialogTexture(texture.filePath)}
                          >
                            {texture.fileName}
                          </Button>
                        ))}
                      </div>
                    )}
                    {formTexture && (
                      <div className="space-y-1 text-xs text-muted-foreground">
                        <div className="break-all">{formTexture.relativePath}</div>
                        <div>
                          {formTexture.originalWidth}x{formTexture.originalHeight} -&gt;{" "}
                          {selectedTexturePreview?.width ?? formTexture.targetWidth}x
                          {selectedTexturePreview?.height ?? formTexture.targetHeight}
                        </div>
                        <div>
                          {formatTextureFormatLabel(formTexture.format)} /{" "}
                          {t(`page.tools.texture_resizer.color_space.${formTexture.colorSpace}`)}
                        </div>
                        <div>
                          {t("page.tools.texture_resizer.current_output_format")}:{" "}
                          {formatTextureFormatLabel(
                            dialogSettings.outputFormat || formTexture.outputFormatDefault,
                          )}
                        </div>
                        {formTexture.formatConversionMessage && (
                          <div>{formTexture.formatConversionMessage}</div>
                        )}
                        {formTexture.message &&
                          isTextureUpscaleOperation(dialogSettings.operation) && (
                            <div>{formTexture.message}</div>
                          )}
                      </div>
                    )}
                    {batchProgress && (
                      <div className="space-y-2 rounded-md border bg-background/40 p-3">
                        <div className="text-xs text-muted-foreground">
                          {runningFilePath
                            ? dialogTextures.find((texture) => texture.filePath === runningFilePath)
                                ?.fileName
                            : null}{" "}
                          {t("page.tools.texture_resizer.batch_progress", {
                            current: batchProgress.current,
                            total: batchProgress.total,
                          })}
                        </div>
                        {upscaleProgress && (
                          <>
                            <div className="text-xs text-muted-foreground">
                              {upscaleProgress.message ??
                                t("page.tools.texture_resizer.upscale_progress.working")}
                            </div>
                            <Progress
                              value={upscaleProgress.percent}
                              className={
                                upscaleProgress.percent == null ? "animate-pulse" : undefined
                              }
                            />
                          </>
                        )}
                      </div>
                    )}
                    <TextureResizerForm
                      settings={dialogSettings}
                      onSettingsChange={(nextSettings) => {
                        dialogSettingsForm.setFieldValue("mode", nextSettings.mode);
                        dialogSettingsForm.setFieldValue("operation", nextSettings.operation);
                        dialogSettingsForm.setFieldValue("percent", nextSettings.percent);
                        dialogSettingsForm.setFieldValue("customWidth", nextSettings.customWidth);
                        dialogSettingsForm.setFieldValue("customHeight", nextSettings.customHeight);
                        dialogSettingsForm.setFieldValue("outputFormat", nextSettings.outputFormat);
                        dialogSettingsForm.setFieldValue("backup", nextSettings.backup);
                        dialogSettingsForm.setFieldValue("upscaleScale", nextSettings.upscaleScale);
                        dialogSettingsForm.setFieldValue("upscaleModel", nextSettings.upscaleModel);
                        if (!bulkApply && activeDialogTexturePath) {
                          setPerTextureSettings((current) => ({
                            ...current,
                            [activeDialogTexturePath]: nextSettings,
                          }));
                        }
                      }}
                      disabled={batchProgress != null}
                      showTargetPath={false}
                      sharedResize={bulkApply && dialogTextures.length > 1}
                      availableOutputFormats={
                        bulkApply && dialogTextures.length > 1
                          ? sharedFormats
                          : formTexture?.availableOutputFormats
                      }
                      currentFormat={
                        bulkApply && dialogTextures.length > 1
                          ? sharedFormats[0]
                          : formTexture?.outputFormatDefault
                      }
                      currentColorSpace={
                        bulkApply && dialogTextures.length > 1
                          ? sharedColor
                          : formTexture?.colorSpace
                      }
                      formatConversionMessage={
                        bulkApply && dialogTextures.length > 1
                          ? sharedFormatConversionMessage(dialogTextures)
                          : formTexture?.formatConversionMessage
                      }
                      resizeSource={
                        formTexture
                          ? {
                              width: formTexture.originalWidth,
                              height: formTexture.originalHeight,
                            }
                          : null
                      }
                    />
                  </div>
                );
              }}
            />
          </ScrollArea>

          <dialogSettingsForm.Subscribe
            selector={(state) => state.values}
            children={(dialogSettings) => {
              const canProcessAny = dialogTextures.some((texture) => {
                const nextSettings =
                  bulkApply || dialogTextures.length === 1
                    ? adaptSettingsForTexture(dialogSettings, texture)
                    : (perTextureSettings[texture.filePath] ?? dialogSettings);
                return canRun(nextSettings, texture);
              });

              return (
                <DialogFooter>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={closeDialog}
                    disabled={batchProgress != null}
                  >
                    {t("g.cancel")}
                  </Button>
                  <Button
                    type="button"
                    className="gap-2"
                    disabled={
                      dialogTextures.length === 0 || batchProgress != null || !canProcessAny
                    }
                    onClick={() => void processSelectedTextures(dialogSettings)}
                  >
                    {batchProgress != null ? (
                      <Loader2Icon className="size-4 animate-spin" />
                    ) : (
                      <ImageIcon className="size-4" />
                    )}
                    {batchProgress != null
                      ? t("page.tools.texture_resizer.running")
                      : dialogTextures.length > 1
                        ? t("page.tools.texture_resizer.process_selected")
                        : t("page.tools.texture_resizer.process_single")}
                  </Button>
                </DialogFooter>
              );
            }}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}

function TextureItemRow({
  texture,
  t,
  selected,
  isRunning,
  disabled,
  onToggle,
}: {
  texture: TextureResizeListItem;
  t: ReturnType<typeof useTranslation>["t"];
  selected: boolean;
  isRunning: boolean;
  disabled: boolean;
  onToggle: (selected: boolean) => void;
}) {
  const formatValue = `${formatTextureFormatLabel(texture.format)} / ${t(
    `page.tools.texture_resizer.color_space.${texture.colorSpace}`,
  )}`;
  const canSelect = texture.canProcess && !disabled;

  return (
    <tr
      className={cn(
        "transition-colors hover:bg-card/50",
        selected && "bg-card/50",
        canSelect && "cursor-pointer",
      )}
      onClick={() => {
        if (!canSelect) {
          return;
        }
        onToggle(!selected);
      }}
    >
      <td className="w-full max-w-0 p-2 pl-3 text-left align-middle whitespace-nowrap">
        <div
          className="block w-full cursor-pointer truncate text-sm font-medium"
          title={texture.fileName}
          onClick={(event) => {
            event.stopPropagation();
            void window.api.invoke("util:openExternal", texture.filePath);
          }}
        >
          {texture.fileName}
        </div>
      </td>
      <td className="w-[1%] p-2 text-right align-middle text-xs whitespace-nowrap text-muted-foreground">
        {formatValue}
      </td>
      <td className="w-[1%] p-2 text-right align-middle text-xs whitespace-nowrap text-muted-foreground">
        {formatSize(texture.fileSize)}
      </td>
      <td className="w-[1%] p-2 text-right align-middle text-xs whitespace-nowrap text-muted-foreground">
        {texture.originalWidth}x{texture.originalHeight}
      </td>
      <td className="w-[1%] p-2 pr-6 text-right align-middle whitespace-nowrap">
        <div className="flex items-center justify-end gap-2">
          {isRunning && <Loader2Icon className="size-4 animate-spin" />}
          <Checkbox
            checked={selected}
            disabled={disabled || !texture.canProcess}
            aria-label={texture.fileName}
            onClick={(event) => event.stopPropagation()}
            onCheckedChange={(checked) => onToggle(checked === true)}
          />
        </div>
      </td>
    </tr>
  );
}

function adaptSettingsForTexture(
  settings: TextureResizeSettings,
  texture: TextureResizeListItem,
): TextureResizeSettings {
  const outputFormat =
    texture.availableOutputFormats.includes(settings.outputFormat) && settings.outputFormat
      ? settings.outputFormat
      : texture.outputFormatDefault;

  if (
    (settings.operation === "resize_and_convert" || settings.operation === "convert") &&
    !texture.canConvertFormat
  ) {
    return {
      ...settings,
      operation: "resize",
      outputFormat,
    };
  }

  if (settings.operation === "upscale_and_convert" && !texture.canConvertFormat) {
    return {
      ...settings,
      operation: "upscale",
      outputFormat,
    };
  }

  return {
    ...settings,
    outputFormat,
  };
}

function buildDialogSettings(
  settings: TextureResizeSettings,
  texture: TextureResizeListItem,
): TextureResizeSettings {
  const adapted = adaptSettingsForTexture(settings, texture);
  const preview = resolveTexturePreview(texture, adapted);

  return {
    ...adapted,
    mode: "custom",
    customWidth: preview?.width ?? adapted.customWidth,
    customHeight: preview?.height ?? adapted.customHeight,
  };
}

function buildSharedDialogSettings(
  settings: TextureResizeSettings,
  textures: TextureResizeListItem[],
): TextureResizeSettings {
  const availableOutputFormats = intersectOutputFormats(textures);
  const outputFormat =
    availableOutputFormats.includes(settings.outputFormat) && settings.outputFormat
      ? settings.outputFormat
      : (availableOutputFormats[0] ?? "");

  return {
    ...settings,
    outputFormat,
  };
}

function intersectOutputFormats(textures: TextureResizeListItem[]): string[] {
  const first = textures[0];
  if (!first) {
    return [];
  }

  return first.availableOutputFormats.filter((format) =>
    textures.every((texture) => texture.availableOutputFormats.includes(format)),
  );
}

function sharedColorSpace(textures: TextureResizeListItem[]): TextureColorSpace {
  if (textures.some((texture) => texture.colorSpace === "linear")) {
    return "linear";
  }

  const first = textures[0]?.colorSpace;
  if (!first) {
    return "unknown";
  }

  return textures.every((texture) => texture.colorSpace === first) ? first : "unknown";
}

function sharedFormatConversionMessage(textures: TextureResizeListItem[]) {
  const first = textures[0]?.formatConversionMessage ?? null;
  if (!first) {
    return null;
  }

  return textures.every((texture) => texture.formatConversionMessage === first) ? first : null;
}

function resolveResizeBounds(texture: TextureResizeListItem, settings: TextureResizeSettings) {
  if (settings.mode === "percent") {
    return {
      width: Math.floor((texture.originalWidth * settings.percent) / 100),
      height: Math.floor((texture.originalHeight * settings.percent) / 100),
    };
  }

  return {
    width: settings.customWidth,
    height: settings.customHeight,
  };
}

function resolveTexturePreview(
  texture: TextureResizeListItem,
  settings: TextureResizeSettings,
): { width: number; height: number } | null {
  if (settings.operation === "convert") {
    return {
      width: texture.originalWidth,
      height: texture.originalHeight,
    };
  }

  if (isTextureUpscaleOperation(settings.operation)) {
    return getTextureUpscaleTarget(
      texture.originalWidth,
      texture.originalHeight,
      settings.upscaleScale,
    );
  }

  const candidates = getTextureResizeCandidates(texture.originalWidth, texture.originalHeight);
  if (candidates.length === 0) {
    return null;
  }

  const bounds = resolveResizeBounds(texture, settings);
  return pickTextureResizeCandidate(candidates, bounds.width, bounds.height);
}

function canUpscaleWithSettings(texture: TextureResizeListItem, settings: TextureResizeSettings) {
  if (texture.layerCount > 1 || isUnsupportedTextureUpscaleFormat(texture.format)) {
    return false;
  }

  return resolveTexturePreview(texture, settings) != null;
}

function canRun(settings: TextureResizeSettings, texture: TextureResizeListItem): boolean {
  const canResizeWithSettings = resolveTexturePreview(texture, settings) != null;

  if (settings.operation === "resize") {
    return canResizeWithSettings;
  }

  if (settings.operation === "convert") {
    return texture.canConvertFormat;
  }

  if (settings.operation === "upscale") {
    return canUpscaleWithSettings(texture, settings);
  }

  if (settings.operation === "upscale_and_convert") {
    return texture.canConvertFormat && canUpscaleWithSettings(texture, settings);
  }

  return canResizeWithSettings && texture.canConvertFormat;
}

function showTextureResizeResultToast(
  t: ReturnType<typeof useTranslation>["t"],
  result: {
    selectedCount: number;
    updated: number;
    failed: number;
    skipped: number;
    lastFileResult?: TextureResizeFileResult;
  },
) {
  const description = t("page.tools.texture_resizer.toast.completed_description", {
    updated: result.updated,
    failed: result.failed,
    skipped: result.skipped,
  });

  if (result.selectedCount === 1 && result.updated === 1) {
    toast.success(t("page.tools.texture_resizer.toast.single_completed"), {
      description: describeFileResult(t, result.lastFileResult),
    });
    return;
  }

  if (result.updated === 0 && result.failed > 0) {
    toast.error(t("page.tools.texture_resizer.toast.failed"), {
      description,
    });
    return;
  }

  if (result.updated === 0) {
    toast.warning(t("page.tools.texture_resizer.toast.none_processed"), {
      description,
    });
    return;
  }

  toast.success(t("page.tools.texture_resizer.toast.completed"), {
    description,
  });
}

function describeFileResult(
  t: ReturnType<typeof useTranslation>["t"],
  fileResult?: TextureResizeFileResult,
) {
  if (!fileResult) {
    return t("page.tools.texture_resizer.toast.single_completed_fallback");
  }

  if (fileResult.status === "updated") {
    return `${fileResult.originalWidth}x${fileResult.originalHeight} -> ${fileResult.outputWidth}x${fileResult.outputHeight}, ${formatTextureFormatLabel(fileResult.originalFormat)} -> ${formatTextureFormatLabel(fileResult.outputFormat)}`;
  }

  return fileResult.message ?? t("page.tools.texture_resizer.toast.single_completed_fallback");
}
