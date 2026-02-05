import { createFileRoute } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Logger } from "@renderer/lib/logger";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Separator } from "@renderer/components/ui/separator";
import { Switch } from "@renderer/components/ui/switch";
import { Input } from "@renderer/components/ui/input";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/mod")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [deleteArchiveAfterExtract, setDeleteArchiveAfterExtract] = useState(false);
  const [moveFolderInsteadOfCopy, setMoveFolderInsteadOfCopy] = useState(false);
  const [virtualizationEnabled, setVirtualizationEnabled] = useState(true);
  const [virtualizationThreshold, setVirtualizationThreshold] = useState(30);
  const [gameFolderCompressionEnabled, setGameFolderCompressionEnabled] = useState(false);
  const [compressionProgress, setCompressionProgress] = useState<{
    message: string;
    processedFiles: number;
    skippedFiles: number;
    errorFiles: number;
  } | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  const [anim1] = useAutoAnimate({ duration: 150 });

  useEffect(() => {
    let timeoutId: NodeJS.Timeout;
    const unlisten = window.api.on("compact:progress", (payload: any) => {
      setCompressionProgress(payload);

      if (timeoutId) clearTimeout(timeoutId);

      const msg = payload.message.toLowerCase();
      if (msg.endsWith("done") || msg.startsWith("auto compressed")) {
        timeoutId = setTimeout(() => {
          setCompressionProgress(null);
        }, 5000);
      }
    });

    return () => {
      unlisten();
      if (timeoutId) clearTimeout(timeoutId);
    };
  }, []);

  useEffect(() => {
    const loadSettings = async () => {
      try {
        const deleteArchive = await window.api.invoke("setting:mod:getDeleteArchiveAfterExtract");
        const moveFolder = await window.api.invoke("setting:mod:getMoveFolderInsteadOfCopy");
        const vEnabled = await window.api.invoke("setting:mod:getVirtualizationEnabled");
        const vThreshold = await window.api.invoke("setting:mod:getVirtualizationThreshold");
        const compressionEnabled = await window.api.invoke(
          "setting:general:getGameFolderCompressionEnabled",
        );

        setDeleteArchiveAfterExtract(deleteArchive);
        setMoveFolderInsteadOfCopy(moveFolder);
        setVirtualizationEnabled(vEnabled);
        setVirtualizationThreshold(vThreshold);
        setGameFolderCompressionEnabled(compressionEnabled);
      } catch (error) {
        Logger.error(error, "ModSettings:loadSettings");
        toast.error("설정을 불러오는데 실패했습니다.");
      } finally {
        setIsLoading(false);
      }
    };

    loadSettings();
  }, []);

  const handleDeleteArchiveChange = async (checked: boolean) => {
    try {
      await window.api.invoke("setting:mod:setDeleteArchiveAfterExtract", checked);
      setDeleteArchiveAfterExtract(checked);
    } catch (error) {
      Logger.error(error, "ModSettings:handleDeleteArchiveChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  const handleMoveFolderChange = async (checked: boolean) => {
    try {
      await window.api.invoke("setting:mod:setMoveFolderInsteadOfCopy", checked);
      setMoveFolderInsteadOfCopy(checked);
    } catch (error) {
      Logger.error(error, "ModSettings:handleMoveFolderChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  const handleVirtualizationEnabledChange = async (checked: boolean) => {
    try {
      await window.api.invoke("setting:mod:setVirtualizationEnabled", checked);
      setVirtualizationEnabled(checked);
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
      await window.api.invoke("setting:mod:setVirtualizationThreshold", value);
      toast.success("설정이 저장되었습니다.");
      setVirtualizationThreshold(value);
      queryClient.invalidateQueries({ queryKey: ["settings", "mod", "virtualization"] });
    } catch (error) {
      Logger.error(error, "ModSettings:handleVirtualizationThresholdChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  const handleCompressionChange = async (checked: boolean) => {
    try {
      await window.api.invoke("setting:general:setGameFolderCompressionEnabled", checked);
      setGameFolderCompressionEnabled(checked);
      toast.success(checked ? "압축 기능이 활성화되었습니다." : "압축 기능이 비활성화되었습니다.");
    } catch (error) {
      Logger.error(error, "ModSettings:handleCompressionChange");
      toast.error("설정 저장에 실패했습니다.");
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-muted-foreground">로딩 중...</p>
      </div>
    );
  }

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
                checked={deleteArchiveAfterExtract}
                onCheckedChange={handleDeleteArchiveChange}
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
              <Switch checked={moveFolderInsteadOfCopy} onCheckedChange={handleMoveFolderChange} />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">
              {t("page.setting.mod.mod_grid_virtualization.title")}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col space-y-4" ref={anim1}>
            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-bold">
                  {t("page.setting.mod.mod_grid_virtualization.useVirtualization")}
                </span>
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.mod.mod_grid_virtualization.useVirtualizationDescription")}
                </p>
              </div>
              <Switch
                checked={virtualizationEnabled}
                onCheckedChange={handleVirtualizationEnabledChange}
              />
            </div>

            {virtualizationEnabled && (
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <span className="text-sm font-bold">
                    {t("page.setting.mod.mod_grid_virtualization.virtualizationThreshold")}
                  </span>
                  <p className="text-xs text-muted-foreground">
                    {t(
                      "page.setting.mod.mod_grid_virtualization.virtualizationThresholdDescription",
                    )}
                  </p>
                </div>

                <Input
                  value={virtualizationThreshold}
                  onChange={(e) => setVirtualizationThreshold(Number(e.target.value))}
                  onBlur={(e) => handleVirtualizationThresholdChange(Number(e.target.value))}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.currentTarget.blur();
                    }
                  }}
                  className="w-20"
                  disabled={!virtualizationEnabled}
                />
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">
              {t("page.setting.mod.gameFolderCompression.title")}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col space-y-4">
            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">
                  {t("page.setting.mod.gameFolderCompression.useCompression")}
                </span>
                <p className="text-sm text-muted-foreground">
                  {t("page.setting.mod.gameFolderCompression.useCompressionDescription")}
                </p>
              </div>
              <Switch
                checked={gameFolderCompressionEnabled}
                onCheckedChange={handleCompressionChange}
              />
            </div>

            {compressionProgress && (
              <div
                className="mt-4 p-3 bg-secondary/30 rounded-lg space-y-2 border border-border"
                ref={anim1}
              >
                <div className="flex justify-between items-center text-xs">
                  <span className="font-semibold text-primary truncate flex-1 mr-2">
                    {compressionProgress.message}
                  </span>
                  <div className="flex space-x-3 text-muted-foreground whitespace-nowrap">
                    <span>처리: {compressionProgress.processedFiles}</span>
                    <span>스킵: {compressionProgress.skippedFiles}</span>
                    {compressionProgress.errorFiles > 0 && (
                      <span className="text-destructive font-bold">
                        에러: {compressionProgress.errorFiles}
                      </span>
                    )}
                  </div>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
