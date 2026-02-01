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

export const Route = createFileRoute("/setting/mod")({
  component: RouteComponent,
});

function RouteComponent() {
  const queryClient = useQueryClient();
  const [deleteArchiveAfterExtract, setDeleteArchiveAfterExtract] = useState(false);
  const [moveFolderInsteadOfCopy, setMoveFolderInsteadOfCopy] = useState(false);
  const [virtualizationEnabled, setVirtualizationEnabled] = useState(true);
  const [virtualizationThreshold, setVirtualizationThreshold] = useState(30);
  const [isLoading, setIsLoading] = useState(true);

  const [anim1] = useAutoAnimate({ duration: 150 });

  useEffect(() => {
    const loadSettings = async () => {
      try {
        const deleteArchive = await window.api.invoke("setting:mod:getDeleteArchiveAfterExtract");
        const moveFolder = await window.api.invoke("setting:mod:getMoveFolderInsteadOfCopy");
        const vEnabled = await window.api.invoke("setting:mod:getVirtualizationEnabled");
        const vThreshold = await window.api.invoke("setting:mod:getVirtualizationThreshold");

        setDeleteArchiveAfterExtract(deleteArchive);
        setMoveFolderInsteadOfCopy(moveFolder);
        setVirtualizationEnabled(vEnabled);
        setVirtualizationThreshold(vThreshold);
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
            <CardTitle className="text-sm font-medium">모드 관리</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col space-y-4">
            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">압축 해제 후 삭제</span>
                <p className="text-sm text-muted-foreground">
                  압축 파일을 모드 폴더에 해제한 후 원본 압축 파일을 삭제합니다
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
                <span className="text-sm font-medium">폴더 복사 대신 이동</span>
                <p className="text-sm text-muted-foreground">
                  폴더를 모드 폴더에 드롭할 때 복사하지 않고 이동합니다 (원본 삭제)
                </p>
              </div>
              <Switch checked={moveFolderInsteadOfCopy} onCheckedChange={handleMoveFolderChange} />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">모드 그리드 가상화</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col space-y-4" ref={anim1}>
            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <span className="text-sm font-bold">가상화 사용</span>
                <p className="text-xs text-muted-foreground">
                  모드 개수가 많을 때 성능을 위해 모드 그리드에 가상화를 적용합니다
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
                  <span className="text-sm font-bold">기준 모드 개수</span>
                  <p className="text-xs text-muted-foreground">
                    설정한 개수 이상의 모드가 있을 때 가상화를 적용합니다
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
      </div>
    </div>
  );
}
