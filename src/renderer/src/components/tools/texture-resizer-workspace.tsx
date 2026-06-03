// oxlint-disable react/no-children-prop
import {
  TextureResizerForm,
  formatTextureFormatLabel,
} from "@renderer/components/tools/texture-resizer-form";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import type {
  TextureResizeFileResult,
  TextureResizeListItem,
  TextureResizeSettings,
} from "@shared/types";
import { formatSize, getTextureResizeCandidates, pickTextureResizeCandidate } from "@shared/utils";
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
};

export function TextureResizerWorkspace({
  mode,
  modName,
  fixedTargetPath,
}: TextureResizerWorkspaceProps) {
  const { t } = useTranslation();
  const [targetPath, setTargetPath] = useState(fixedTargetPath ?? "");
  const [textures, setTextures] = useState<TextureResizeListItem[]>([]);
  const [isListing, setIsListing] = useState(false);
  const [runningFilePath, setRunningFilePath] = useState<string | null>(null);
  const [loadedTargetPath, setLoadedTargetPath] = useState("");
  const [selectedTexture, setSelectedTexture] = useState<TextureResizeListItem | null>(null);
  const settingsForm = useForm({
    defaultValues: DEFAULT_SETTINGS,
    onSubmit: async () => {},
  });
  const dialogSettingsForm = useForm({
    defaultValues: DEFAULT_SETTINGS,
    onSubmit: async () => {},
  });

  useEffect(() => {
    setTargetPath(fixedTargetPath ?? "");
  }, [fixedTargetPath]);

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
          description: error instanceof Error ? error.message : String(error),
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
    } catch (error) {
      toast.error(t("page.tools.texture_resizer.toast.load_failed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setIsListing(false);
    }
  };

  const processTexture = async (filePath: string, nextSettings: TextureResizeSettings) => {
    if (runningFilePath) {
      return;
    }

    setRunningFilePath(filePath);
    try {
      settingsForm.reset(nextSettings);
      const nextResult = await window.api.invoke("tools:resizeTextureFile", {
        filePath,
        settings: nextSettings,
      });
      const fileResult = nextResult.files[0];
      toast.success(t("page.tools.texture_resizer.toast.single_completed"), {
        description: describeFileResult(t, fileResult),
      });
      setSelectedTexture(null);
      if (loadedTargetPath) {
        await loadTextures(loadedTargetPath, nextSettings);
      }
    } catch (error) {
      toast.error(t("page.tools.texture_resizer.toast.failed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setRunningFilePath(null);
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
            disabled={isListing || runningFilePath !== null}
          />
          <Button
            type="button"
            variant="outline"
            className="shrink-0 gap-1"
            onClick={() => void browseTargetPath()}
            disabled={isListing || runningFilePath !== null}
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
            disabled={!hasTarget || isListing || runningFilePath !== null}
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
            disabled={
              isListing || runningFilePath !== null || (!loadedTargetPath && mode !== "mod")
            }
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
                <tbody>
                  {textures.map((texture, index) => (
                    <Fragment key={texture.filePath}>
                      <TextureItemRow
                        texture={texture}
                        t={t}
                        isRunning={runningFilePath === texture.filePath}
                        disabled={isListing || runningFilePath !== null}
                        onProcess={() => {
                          setSelectedTexture(texture);
                          dialogSettingsForm.reset(
                            buildDialogSettings(settingsForm.state.values, texture),
                          );
                        }}
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
        open={selectedTexture != null}
        onOpenChange={(open) => {
          if (!open && runningFilePath == null) {
            setSelectedTexture(null);
          }
        }}
      >
        <DialogContent className="grid min-w-xl max-h-[80vh] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden">
          <DialogHeader>
            <DialogTitle>{t("page.tools.texture_resizer.dialog.title")}</DialogTitle>
            <DialogDescription>
              {selectedTexture?.fileName ??
                t("page.tools.texture_resizer.dialog.description_fallback")}
            </DialogDescription>
          </DialogHeader>

          <ScrollArea className="min-h-0">
            <dialogSettingsForm.Subscribe
              selector={(state) => state.values}
              children={(dialogSettings) => {
                const selectedTexturePreview =
                  selectedTexture != null
                    ? resolveTexturePreview(selectedTexture, dialogSettings)
                    : null;

                return (
                  <div className="space-y-3 pr-4">
                    {selectedTexture && (
                      <div className="space-y-1 text-xs text-muted-foreground">
                        <div className="break-all">{selectedTexture.relativePath}</div>
                        <div>
                          {selectedTexture.originalWidth}x{selectedTexture.originalHeight} -&gt;{" "}
                          {selectedTexturePreview?.width ?? selectedTexture.targetWidth}x
                          {selectedTexturePreview?.height ?? selectedTexture.targetHeight}
                        </div>
                        <div>
                          {formatTextureFormatLabel(selectedTexture.format)} /{" "}
                          {t(
                            `page.tools.texture_resizer.color_space.${selectedTexture.colorSpace}`,
                          )}
                        </div>
                        <div>
                          {t("page.tools.texture_resizer.current_output_format")}:{" "}
                          {formatTextureFormatLabel(
                            dialogSettings.outputFormat || selectedTexture.outputFormatDefault,
                          )}
                        </div>
                        {selectedTexture.formatConversionMessage && (
                          <div>{selectedTexture.formatConversionMessage}</div>
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
                      }}
                      disabled={runningFilePath != null}
                      showTargetPath={false}
                      availableOutputFormats={selectedTexture?.availableOutputFormats}
                      currentFormat={selectedTexture?.outputFormatDefault}
                      currentColorSpace={selectedTexture?.colorSpace}
                      formatConversionMessage={selectedTexture?.formatConversionMessage}
                      resizeSource={
                        selectedTexture
                          ? {
                              width: selectedTexture.originalWidth,
                              height: selectedTexture.originalHeight,
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
            children={(dialogSettings) => (
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setSelectedTexture(null)}
                  disabled={runningFilePath != null}
                >
                  {t("g.cancel")}
                </Button>
                <Button
                  type="button"
                  className="gap-2"
                  disabled={
                    !selectedTexture ||
                    runningFilePath != null ||
                    !canRun(dialogSettings, selectedTexture)
                  }
                  onClick={() =>
                    selectedTexture && void processTexture(selectedTexture.filePath, dialogSettings)
                  }
                >
                  {runningFilePath === selectedTexture?.filePath ? (
                    <Loader2Icon className="size-4 animate-spin" />
                  ) : (
                    <ImageIcon className="size-4" />
                  )}
                  {runningFilePath === selectedTexture?.filePath
                    ? t("page.tools.texture_resizer.running")
                    : t("page.tools.texture_resizer.process_single")}
                </Button>
              </DialogFooter>
            )}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}

function TextureItemRow({
  texture,
  t,
  isRunning,
  disabled,
  onProcess,
}: {
  texture: TextureResizeListItem;
  t: ReturnType<typeof useTranslation>["t"];
  isRunning: boolean;
  disabled: boolean;
  onProcess: () => void;
}) {
  const formatValue = `${formatTextureFormatLabel(texture.format)} / ${t(
    `page.tools.texture_resizer.color_space.${texture.colorSpace}`,
  )}`;

  return (
    <tr className="transition-colors hover:bg-card/50">
      <td className="w-full max-w-0 p-2 pl-3 text-left align-middle whitespace-nowrap">
        <div
          className="block w-full cursor-pointer truncate text-sm font-medium"
          title={texture.fileName}
          onClick={() => {
            void window.api.invoke("util:openExternal", texture.filePath);
          }}
        >
          {texture.fileName}
        </div>
      </td>
      <td className="w-[1%] p-2 text-right align-middle whitespace-nowrap text-xs text-muted-foreground">
        {formatValue}
      </td>
      <td className="w-[1%] p-2 text-right align-middle whitespace-nowrap text-xs text-muted-foreground">
        {formatSize(texture.fileSize)}
      </td>
      <td className="w-[1%] p-2 text-right align-middle whitespace-nowrap text-xs text-muted-foreground">
        {texture.originalWidth}x{texture.originalHeight}
      </td>
      <td className="w-[1%] p-2 pr-6 text-right align-middle whitespace-nowrap">
        <Button
          size="sm"
          className="gap-2"
          onClick={onProcess}
          disabled={disabled || !texture.canProcess}
        >
          {isRunning ? (
            <Loader2Icon className="size-4 animate-spin" />
          ) : (
            <ImageIcon className="size-4" />
          )}
          {isRunning
            ? t("page.tools.texture_resizer.running")
            : t("page.tools.texture_resizer.process_single")}
        </Button>
      </td>
    </tr>
  );
}

function buildDialogSettings(
  settings: TextureResizeSettings,
  texture: TextureResizeListItem,
): TextureResizeSettings {
  const outputFormat =
    texture.availableOutputFormats.includes(settings.outputFormat) && settings.outputFormat
      ? settings.outputFormat
      : texture.outputFormatDefault;

  let operation = settings.operation;
  if (operation !== "resize" && !texture.canConvertFormat) {
    operation = "resize";
  }

  return {
    ...settings,
    mode: "custom",
    customWidth: texture.canResize ? texture.targetWidth : settings.customWidth,
    customHeight: texture.canResize ? texture.targetHeight : settings.customHeight,
    operation,
    outputFormat,
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

  const candidates = getTextureResizeCandidates(texture.originalWidth, texture.originalHeight);
  if (candidates.length === 0) {
    return null;
  }

  return pickTextureResizeCandidate(candidates, settings.customWidth, settings.customHeight);
}

function canRun(settings: TextureResizeSettings, texture: TextureResizeListItem): boolean {
  const canResizeWithSettings = resolveTexturePreview(texture, settings) != null;

  if (settings.operation === "resize") {
    return canResizeWithSettings;
  }

  if (settings.operation === "convert") {
    return texture.canConvertFormat;
  }

  return canResizeWithSettings && texture.canConvertFormat;
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
