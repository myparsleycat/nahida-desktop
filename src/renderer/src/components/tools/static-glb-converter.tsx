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
import { Switch } from "@renderer/components/ui/switch";
import { useNavigate } from "@tanstack/react-router";
import { BoxIcon, CircleCheckIcon, CircleXIcon, FolderOpenIcon, Loader2Icon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ScrollArea } from "../ui/scroll-area";

type TextureFormat = "auto" | "png" | "jpeg-safe" | "jpeg-force";

const JPEG_TEXTURE_FORMATS: TextureFormat[] = ["auto", "jpeg-safe", "jpeg-force"];

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
  const [assetPath, setAssetPath] = useState("");
  const [modPath, setModPath] = useState("");
  const [outputPath, setOutputPath] = useState("");
  const [textureFormat, setTextureFormat] = useState<TextureFormat>("auto");
  const [jpegQuality, setJpegQuality] = useState(85);
  const [includeTangents, setIncludeTangents] = useState(false);
  const [debug, setDebug] = useState(false);
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

  useEffect(() => {
    Promise.all([
      window.api.invoke("tools:getStaticGlbAssetPath"),
      window.api.invoke("tools:getStaticGlbTextureSettings"),
    ])
      .then(([nextAssetPath, settings]) => {
        setAssetPath(nextAssetPath);
        setTextureFormat(settings.textureFormat);
        setJpegQuality(clampJpegQuality(settings.jpegQuality));
      })
      .catch(() => {});
  }, []);

  const canConvert = useMemo(
    () => assetPath.trim().length > 0 && modPath.trim().length > 0 && outputPath.trim().length > 0,
    [assetPath, modPath, outputPath],
  );
  const usesJpeg = JPEG_TEXTURE_FORMATS.includes(textureFormat);

  const selectFolder = async (onSelect: (path: string) => void) => {
    const selected = await window.api.invoke("util:showOpenDialog", {
      properties: ["openDirectory"],
    });
    const filePath = selected.filePaths[0];
    if (filePath) onSelect(filePath);
  };

  const selectAssetPath = () => {
    void selectFolder((filePath) => {
      setAssetPath(filePath);
      window.api.invoke("tools:setStaticGlbAssetPath", filePath).catch((error) => {
        toast.error(t("page.tools.static_glb_converter.toast.save_asset_path_failed"), {
          description: error.message,
        });
      });
    });
  };

  const selectModPath = () => {
    void selectFolder((filePath) => {
      setModPath(filePath);
      if (!outputPath) {
        setOutputPath(joinPath(filePath, `${basename(filePath)}.glb`));
      }
    });
  };

  const selectOutputFolder = () => {
    void selectFolder((filePath) => {
      const name = basename(modPath || filePath);
      setOutputPath(joinPath(filePath, `${name}.glb`));
    });
  };

  const convert = async () => {
    if (!canConvert || isRunning) return;

    setIsRunning(true);
    setResult(null);
    try {
      const nextResult = await window.api.invoke("tools:convertStaticGlb", {
        modPath,
        assetPath,
        outputPath,
        textureFormat,
        jpegQuality,
        includeTangents,
        debug,
      });
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
  };

  const handleTextureFormatChange = async (value: TextureFormat) => {
    const previous = textureFormat;
    setTextureFormat(value);

    try {
      const saved = await window.api.invoke("tools:setStaticGlbTextureFormat", value);
      setTextureFormat(saved);
    } catch (error) {
      setTextureFormat(previous);
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

    const previous = jpegQuality;
    const normalized = clampJpegQuality(parsed);
    setJpegQuality(normalized);

    try {
      const saved = await window.api.invoke("tools:setStaticGlbJpegQuality", normalized);
      setJpegQuality(clampJpegQuality(saved));
    } catch (error) {
      setJpegQuality(previous);
      toast.error(t("page.tools.static_glb_converter.toast.save_texture_settings_failed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    }
  };

  const openResult = () => {
    if (!result) return;
    navigate({
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
      <div className="flex h-full min-h-0 flex-col space-y-3 p-4">
        <div className="grid gap-4 rounded-lg border bg-card p-4">
          <div className="space-y-2">
            <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
              {t("page.tools.static_glb_converter.asset_layout_path")}
            </label>
            <div className="flex gap-2">
              <Input
                value={assetPath}
                onChange={(e) => setAssetPath(e.target.value)}
                placeholder={t("page.tools.static_glb_converter.asset_layout_placeholder")}
                disabled={isRunning}
              />
              <Button
                variant="outline"
                className="shrink-0 gap-1"
                onClick={selectAssetPath}
                disabled={isRunning}
              >
                <FolderOpenIcon className="size-4" />
                {t("page.tools.static_glb_converter.browse")}
              </Button>
            </div>
            {/* <p className="text-xs text-muted-foreground">
            {t("page.tools.static_glb_converter.asset_layout_hint")}
          </p> */}
          </div>

          <div className="space-y-2">
            <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
              {t("page.tools.static_glb_converter.target_mod_path")}
            </label>
            <div className="flex gap-2">
              <Input
                value={modPath}
                onChange={(e) => setModPath(e.target.value)}
                placeholder={t("page.tools.static_glb_converter.target_mod_placeholder")}
                disabled={isRunning}
              />
              <Button
                variant="outline"
                className="shrink-0 gap-1"
                onClick={selectModPath}
                disabled={isRunning}
              >
                <FolderOpenIcon className="size-4" />
                {t("page.tools.static_glb_converter.browse")}
              </Button>
            </div>
          </div>

          <div className="space-y-2">
            <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
              {t("page.tools.static_glb_converter.output_glb_path")}
            </label>
            <div className="flex gap-2">
              <Input
                value={outputPath}
                onChange={(e) => setOutputPath(e.target.value)}
                placeholder={t("page.tools.static_glb_converter.output_glb_placeholder")}
                disabled={isRunning}
              />
              <Button
                variant="outline"
                className="shrink-0 gap-1"
                onClick={selectOutputFolder}
                disabled={isRunning}
              >
                <FolderOpenIcon className="size-4" />
                {t("page.tools.static_glb_converter.folder")}
              </Button>
            </div>
          </div>

          <div className="grid gap-4 rounded-md border bg-background/40 p-3 md:grid-cols-[minmax(0,1fr)_160px]">
            <div className="space-y-2">
              <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                {t("page.tools.static_glb_converter.texture_format")}
              </label>
              <Select
                value={textureFormat}
                onValueChange={(value) => void handleTextureFormatChange(value as TextureFormat)}
              >
                <SelectTrigger disabled={isRunning}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectGroup>
                    <SelectItem value="auto">
                      {t("page.tools.static_glb_converter.texture_format_options.auto")}
                    </SelectItem>
                    <SelectItem value="png">
                      {t("page.tools.static_glb_converter.texture_format_options.png")}
                    </SelectItem>
                    <SelectItem value="jpeg-safe">
                      {t("page.tools.static_glb_converter.texture_format_options.jpeg_safe")}
                    </SelectItem>
                    <SelectItem value="jpeg-force">
                      {t("page.tools.static_glb_converter.texture_format_options.jpeg_force")}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {t(`page.tools.static_glb_converter.texture_format_descriptions.${textureFormat}`)}
              </p>
            </div>

            <div className="space-y-2">
              <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                {t("page.tools.static_glb_converter.jpeg_quality")}
              </label>
              <Input
                type="number"
                min={1}
                max={100}
                step={1}
                value={jpegQuality}
                onChange={(e) => void handleJpegQualityChange(e.target.value)}
                disabled={isRunning || !usesJpeg}
              />
              <p className="text-xs text-muted-foreground">
                {usesJpeg
                  ? t("page.tools.static_glb_converter.jpeg_quality_hint")
                  : t("page.tools.static_glb_converter.jpeg_quality_disabled_hint")}
              </p>
            </div>
          </div>

          <div className="flex items-center justify-between rounded-md border bg-background/40 p-3">
            <div>
              <div className="text-sm font-medium">
                {t("page.tools.static_glb_converter.include_tangents")}
              </div>
              <div className="text-xs text-muted-foreground">
                {t("page.tools.static_glb_converter.include_tangents_description")}
              </div>
            </div>
            <Switch
              checked={includeTangents}
              onCheckedChange={setIncludeTangents}
              disabled={isRunning}
            />
          </div>

          <div className="flex items-center justify-between rounded-md border bg-background/40 p-3">
            <div>
              <div className="text-sm font-medium">
                {t("page.tools.static_glb_converter.debug_mode")}
              </div>
              {/*<div className="text-xs text-muted-foreground">
              {t("page.tools.static_glb_converter.debug_mode_description")}
            </div>*/}
            </div>
            <Switch checked={debug} onCheckedChange={setDebug} disabled={isRunning} />
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button onClick={convert} disabled={!canConvert || isRunning} className="gap-2">
            {isRunning ? (
              <Loader2Icon className="size-4 animate-spin" />
            ) : (
              <BoxIcon className="size-4" />
            )}
            {isRunning
              ? t("page.tools.static_glb_converter.converting")
              : t("page.tools.static_glb_converter.convert_to_glb")}
          </Button>
          <Button variant="outline" onClick={openResult} disabled={!result || isRunning}>
            {t("page.tools.static_glb_converter.open_in_model_viewer")}
          </Button>
        </div>

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
      </div>
    </ScrollArea>
  );
}
