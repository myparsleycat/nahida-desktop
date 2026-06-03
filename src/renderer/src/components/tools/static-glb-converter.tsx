// oxlint-disable react/no-children-prop
import { Button } from "@renderer/components/ui/button";
import { Field, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import { useForm } from "@tanstack/react-form";
import { useNavigate } from "@tanstack/react-router";
import { BoxIcon, CircleCheckIcon, CircleXIcon, FolderOpenIcon, Loader2Icon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ScrollArea } from "../ui/scroll-area";

type TextureFormat = "png" | "jpeg-safe" | "jpeg-force";

const JPEG_TEXTURE_FORMATS: TextureFormat[] = ["jpeg-safe", "jpeg-force"];

function clampJpegQuality(value: number) {
  if (!Number.isFinite(value)) {
    return 85;
  }

  return Math.max(1, Math.min(100, Math.round(value)));
}

function basename(filePath: string) {
  return filePath.replaceAll("\\", "/").split("/").filter(Boolean).at(-1) || "mod";
}

function joinPath(dir: string, name: string) {
  const separator = dir.includes("\\") ? "\\" : "/";
  return `${dir.replace(/[\\/]+$/, "")}${separator}${name}`;
}

export default function StaticGlbConverter() {
  const navigate = useNavigate();
  const { t } = useTranslation();
  const [isRunning, setIsRunning] = useState(false);
  const [result, setResult] = useState<{
    mode: "single" | "variant-set";
    glbPath: string;
    meshCount: number;
    warningCount: number;
    name: string;
    manifestPath?: string;
    artifactRoot?: string;
  } | null>(null);
  const form = useForm({
    defaultValues: {
      assetPath: "",
      modPath: "",
      outputPath: "",
      textureFormat: "jpeg-safe" as TextureFormat,
      jpegQuality: 85,
      includeTangents: false,
      debug: false,
    },
    onSubmit: async ({ value }) => {
      if (!canConvertStaticGlb(value) || isRunning) {
        return;
      }

      setIsRunning(true);
      setResult(null);
      try {
        const nextResult = await window.api.invoke("tools:convertStaticGlb", value);
        setResult(nextResult);
        toast.success(t("page.tools.static_glb_converter.toast.created"), {
          description: t("page.tools.static_glb_converter.toast.created_description", {
            meshCount: nextResult.meshCount,
            warningCount: nextResult.warningCount,
          }),
        });
      } catch (error) {
        toast.error(t("page.tools.static_glb_converter.toast.failed"), {
          description: error instanceof Error ? error.message : String(error),
        });
      } finally {
        setIsRunning(false);
      }
    },
  });

  useEffect(() => {
    Promise.all([
      window.api.invoke("tools:getStaticGlbAssetPath"),
      window.api.invoke("tools:getStaticGlbTextureSettings"),
    ])
      .then(([nextAssetPath, settings]) => {
        form.reset({
          assetPath: nextAssetPath,
          modPath: "",
          outputPath: "",
          textureFormat: settings.textureFormat,
          jpegQuality: clampJpegQuality(settings.jpegQuality),
          includeTangents: false,
          debug: false,
        });
      })
      .catch(() => {});
  }, [form]);

  const selectFolder = async (onSelect: (path: string) => void) => {
    const selected = await window.api.invoke("util:showOpenDialog", {
      properties: ["openDirectory"],
    });
    const filePath = selected.filePaths[0];
    if (filePath) onSelect(filePath);
  };

  const selectAssetPath = () => {
    void selectFolder((filePath) => {
      form.setFieldValue("assetPath", filePath);
      window.api.invoke("tools:setStaticGlbAssetPath", filePath).catch((error) => {
        toast.error(t("page.tools.static_glb_converter.toast.save_asset_path_failed"), {
          description: error.message,
        });
      });
    });
  };

  const selectModPath = () => {
    void selectFolder((filePath) => {
      form.setFieldValue("modPath", filePath);
      if (!form.state.values.outputPath) {
        form.setFieldValue("outputPath", joinPath(filePath, `${basename(filePath)}.glb`));
      }
    });
  };

  const selectOutputFolder = () => {
    void selectFolder((filePath) => {
      const name = basename(form.state.values.modPath || filePath);
      form.setFieldValue("outputPath", joinPath(filePath, `${name}.glb`));
    });
  };

  const handleTextureFormatChange = async (value: TextureFormat) => {
    const previous = form.state.values.textureFormat;
    form.setFieldValue("textureFormat", value);

    try {
      const saved = await window.api.invoke("tools:setStaticGlbTextureFormat", value);
      form.setFieldValue("textureFormat", saved);
    } catch (error) {
      form.setFieldValue("textureFormat", previous);
      toast.error(t("page.tools.static_glb_converter.toast.save_texture_settings_failed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    }
  };

  const handleJpegQualityChange = async (value: string) => {
    const parsed = Number.parseInt(value, 10);
    if (Number.isNaN(parsed)) {
      return;
    }

    const previous = form.state.values.jpegQuality;
    const normalized = clampJpegQuality(parsed);
    form.setFieldValue("jpegQuality", normalized);

    try {
      const saved = await window.api.invoke("tools:setStaticGlbJpegQuality", normalized);
      form.setFieldValue("jpegQuality", clampJpegQuality(saved));
    } catch (error) {
      form.setFieldValue("jpegQuality", previous);
      toast.error(t("page.tools.static_glb_converter.toast.save_texture_settings_failed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    }
  };

  const openResult = () => {
    if (!result) return;
    void navigate({
      to: "/tools/model-viewer",
      search: {
        path: result.glbPath,
        name: result.name,
        manifestPath: result.manifestPath ?? "",
        artifactRoot: result.artifactRoot ?? "",
      },
    });
  };

  return (
    <ScrollArea className="h-full">
      <form
        className="flex h-full min-h-0 flex-col space-y-3 p-4"
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          void form.handleSubmit();
        }}
      >
        <form.Subscribe
          selector={(state) => state.values}
          children={(values) => {
            const canConvert = canConvertStaticGlb(values);
            const usesJpeg = JPEG_TEXTURE_FORMATS.includes(values.textureFormat);

            return (
              <>
                <div className="grid gap-4 rounded-lg border bg-card p-4">
                  <form.Field
                    name="assetPath"
                    children={(field) => (
                      <Field>
                        <FieldLabel className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                          {t("page.tools.static_glb_converter.asset_layout_path")}
                        </FieldLabel>
                        <div className="flex gap-2">
                          <Input
                            value={field.state.value}
                            onBlur={field.handleBlur}
                            onChange={(e) => field.handleChange(e.target.value)}
                            placeholder={t(
                              "page.tools.static_glb_converter.asset_layout_placeholder",
                            )}
                            disabled={isRunning}
                          />
                          <Button
                            type="button"
                            variant="outline"
                            className="shrink-0 gap-1"
                            onClick={selectAssetPath}
                            disabled={isRunning}
                          >
                            <FolderOpenIcon className="size-4" />
                            {t("page.tools.static_glb_converter.browse")}
                          </Button>
                        </div>
                      </Field>
                    )}
                  />

                  <form.Field
                    name="modPath"
                    children={(field) => (
                      <Field>
                        <FieldLabel className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                          {t("page.tools.static_glb_converter.target_mod_path")}
                        </FieldLabel>
                        <div className="flex gap-2">
                          <Input
                            value={field.state.value}
                            onBlur={field.handleBlur}
                            onChange={(e) => field.handleChange(e.target.value)}
                            placeholder={t(
                              "page.tools.static_glb_converter.target_mod_placeholder",
                            )}
                            disabled={isRunning}
                          />
                          <Button
                            type="button"
                            variant="outline"
                            className="shrink-0 gap-1"
                            onClick={selectModPath}
                            disabled={isRunning}
                          >
                            <FolderOpenIcon className="size-4" />
                            {t("page.tools.static_glb_converter.browse")}
                          </Button>
                        </div>
                      </Field>
                    )}
                  />

                  <form.Field
                    name="outputPath"
                    children={(field) => (
                      <Field>
                        <FieldLabel className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                          {t("page.tools.static_glb_converter.output_glb_path")}
                        </FieldLabel>
                        <div className="flex gap-2">
                          <Input
                            value={field.state.value}
                            onBlur={field.handleBlur}
                            onChange={(e) => field.handleChange(e.target.value)}
                            placeholder={t(
                              "page.tools.static_glb_converter.output_glb_placeholder",
                            )}
                            disabled={isRunning}
                          />
                          <Button
                            type="button"
                            variant="outline"
                            className="shrink-0 gap-1"
                            onClick={selectOutputFolder}
                            disabled={isRunning}
                          >
                            <FolderOpenIcon className="size-4" />
                            {t("page.tools.static_glb_converter.folder")}
                          </Button>
                        </div>
                      </Field>
                    )}
                  />

                  <div className="grid gap-4 rounded-md border bg-background/40 p-3 md:grid-cols-[minmax(0,1fr)_160px]">
                    <form.Field
                      name="textureFormat"
                      children={(field) => (
                        <Field>
                          <FieldLabel className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                            {t("page.tools.static_glb_converter.texture_format")}
                          </FieldLabel>
                          <Select
                            value={field.state.value}
                            onValueChange={(value) =>
                              void handleTextureFormatChange(value as TextureFormat)
                            }
                          >
                            <SelectTrigger disabled={isRunning}>
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent position="popper">
                              <SelectGroup>
                                <SelectItem value="png">
                                  {t("page.tools.static_glb_converter.texture_format_options.png")}
                                </SelectItem>
                                <SelectItem value="jpeg-safe">
                                  {t(
                                    "page.tools.static_glb_converter.texture_format_options.jpeg_safe",
                                  )}
                                </SelectItem>
                                <SelectItem value="jpeg-force">
                                  {t(
                                    "page.tools.static_glb_converter.texture_format_options.jpeg_force",
                                  )}
                                </SelectItem>
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                          <p className="text-xs text-muted-foreground">
                            {t(
                              `page.tools.static_glb_converter.texture_format_descriptions.${field.state.value}`,
                            )}
                          </p>
                        </Field>
                      )}
                    />

                    <form.Field
                      name="jpegQuality"
                      children={(field) => (
                        <Field>
                          <FieldLabel className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                            {t("page.tools.static_glb_converter.jpeg_quality")}
                          </FieldLabel>
                          <Input
                            type="number"
                            min={1}
                            max={100}
                            step={1}
                            value={field.state.value}
                            onBlur={field.handleBlur}
                            onChange={(e) => void handleJpegQualityChange(e.target.value)}
                            disabled={isRunning || !usesJpeg}
                          />
                        </Field>
                      )}
                    />
                  </div>

                  <div className="flex w-full flex-row gap-2">
                    <form.Field
                      name="includeTangents"
                      children={(field) => (
                        <div className="flex flex-1 items-center justify-between rounded-md border bg-background/40 p-3">
                          <div>
                            <div className="text-sm font-medium">
                              {t("page.tools.static_glb_converter.include_tangents")}
                            </div>
                            <div className="text-xs text-muted-foreground">
                              {t("page.tools.static_glb_converter.include_tangents_description")}
                            </div>
                          </div>
                          <Switch
                            checked={field.state.value}
                            onCheckedChange={field.handleChange}
                            disabled={isRunning}
                          />
                        </div>
                      )}
                    />

                    <form.Field
                      name="debug"
                      children={(field) => (
                        <div className="flex flex-1 items-center justify-between rounded-md border bg-background/40 p-3">
                          <div>
                            <div className="text-sm font-medium">
                              {t("page.tools.static_glb_converter.debug_mode")}
                            </div>
                          </div>
                          <Switch
                            checked={field.state.value}
                            onCheckedChange={field.handleChange}
                            disabled={isRunning}
                          />
                        </div>
                      )}
                    />
                  </div>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <Button type="submit" disabled={!canConvert || isRunning} className="gap-2">
                    {isRunning ? (
                      <Loader2Icon className="size-4 animate-spin" />
                    ) : (
                      <BoxIcon className="size-4" />
                    )}
                    {isRunning
                      ? t("page.tools.static_glb_converter.converting")
                      : t("page.tools.static_glb_converter.convert_to_glb")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={openResult}
                    disabled={!result || isRunning}
                  >
                    {t("page.tools.static_glb_converter.open_in_model_viewer")}
                  </Button>
                </div>
              </>
            );
          }}
        />

        {result && (
          <div className="flex items-start gap-2 rounded-lg border bg-card p-3 text-sm">
            {result.warningCount > 0 ? (
              <CircleXIcon className="mt-0.5 size-4 shrink-0 text-yellow-500" />
            ) : (
              <CircleCheckIcon className="mt-0.5 size-4 shrink-0 text-green-500" />
            )}
            <div className="min-w-0">
              <div className="font-medium">
                {t("page.tools.static_glb_converter.result_written", {
                  meshCount: result.meshCount,
                })}
              </div>
              <div className="truncate text-xs text-muted-foreground">
                {result.mode === "variant-set" ? result.artifactRoot : result.glbPath}
              </div>
              {result.warningCount > 0 && (
                <div className="mt-1 text-xs text-yellow-500">
                  {t("page.tools.static_glb_converter.result_warnings", {
                    warningCount: result.warningCount,
                  })}
                </div>
              )}
            </div>
          </div>
        )}
      </form>
    </ScrollArea>
  );
}

function canConvertStaticGlb(values: { assetPath: string; modPath: string; outputPath: string }) {
  return (
    values.assetPath.trim().length > 0 &&
    values.modPath.trim().length > 0 &&
    values.outputPath.trim().length > 0
  );
}
