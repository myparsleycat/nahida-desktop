import { createFileRoute } from "@tanstack/react-router";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Logger } from "@renderer/lib/logger";
import { Card, CardContent } from "@renderer/components/ui/card";
import { Separator } from "@renderer/components/ui/separator";

export const Route = createFileRoute("/setting/mod")({
  component: RouteComponent,
});

function RouteComponent() {
  const [deleteArchiveAfterExtract, setDeleteArchiveAfterExtract] = useState(false);
  const [moveFolderInsteadOfCopy, setMoveFolderInsteadOfCopy] = useState(false);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const loadSettings = async () => {
      try {
        const deleteArchive = await window.api.invoke("setting:mod:getDeleteArchiveAfterExtract");
        const moveFolder = await window.api.invoke("setting:mod:getMoveFolderInsteadOfCopy");

        setDeleteArchiveAfterExtract(deleteArchive);
        setMoveFolderInsteadOfCopy(moveFolder);
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
          <CardContent className="flex flex-col space-y-4">
            <label className="flex items-start gap-3 cursor-pointer">
              <Checkbox
                id="delete-archive"
                checked={deleteArchiveAfterExtract}
                onCheckedChange={(checked) => handleDeleteArchiveChange(checked as boolean)}
                className="mt-1"
              />
              <div className="flex-1 space-y-1">
                <span className="text-sm font-medium">압축 해제 후 삭제</span>
                <p className="text-sm text-muted-foreground">
                  압축 파일을 모드 폴더에 해제한 후 원본 압축 파일을 삭제합니다
                </p>
              </div>
            </label>

            <Separator />

            <label className="flex items-start gap-3 cursor-pointer">
              <Checkbox
                id="move-folder"
                checked={moveFolderInsteadOfCopy}
                onCheckedChange={(checked) => handleMoveFolderChange(checked as boolean)}
                className="mt-1"
              />
              <div className="flex-1 space-y-1">
                <span className="text-sm font-medium">폴더 복사 대신 이동</span>
                <p className="text-sm text-muted-foreground">
                  폴더를 모드 폴더에 드롭할 때 복사하지 않고 이동합니다 (원본 삭제)
                </p>
              </div>
            </label>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
