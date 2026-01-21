import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { Download, X } from "lucide-react";
import { useState, useEffect } from "react";

interface DownloadConfirmationOverlayProps {
  selectedPath: string | null;
  selectedGroupName: string | null;
  suggestedName?: string;
  onConfirm: (fileName: string) => void;
  onCancel: () => void;
}

export function DownloadConfirmationOverlay({
  selectedPath,
  selectedGroupName,
  suggestedName,
  onConfirm,
  onCancel,
}: DownloadConfirmationOverlayProps) {
  const [fileName, setFileName] = useState(suggestedName || "");

  useEffect(() => {
    setFileName(suggestedName || "");
  }, [suggestedName]);

  const handleConfirm = () => {
    if (fileName.trim()) {
      onConfirm(fileName.trim());
    }
  };

  return (
    <div
      className="fixed right-0 bottom-0 z-50 flex items-center justify-center bg-black/20 backdrop-blur-[1px]"
      style={{ left: "309px", top: "76px" }}
      onClick={(e) => {
        e.stopPropagation();
      }}
    >
      <div
        className="bg-background border rounded-lg p-4 max-w-md w-full mx-4 shadow-xl"
        onClick={(e) => {
          e.stopPropagation();
        }}
      >
        <div className="space-y-4">
          <div className="flex items-start justify-between">
            <div>
              <h3 className="text-lg font-semibold">다운로드 확인</h3>
              <p className="text-sm text-muted-foreground mt-1">다운로드할 위치를 선택하세요</p>
            </div>
          </div>

          {suggestedName && (
            <div className="space-y-1">
              <p className="text-sm font-medium">파일 이름:</p>
              <Input
                value={fileName}
                onChange={(e) => setFileName(e.target.value)}
                placeholder="파일 이름을 입력하세요"
                className="w-full"
              />
            </div>
          )}

          <div className="space-y-1">
            <p className="text-sm font-medium">다운로드 위치:</p>
            {selectedGroupName ? (
              <p className="text-sm text-muted-foreground bg-muted px-3 py-2 rounded">
                {selectedGroupName}
              </p>
            ) : (
              <p className="text-sm text-muted-foreground italic">
                좌측에서 캐릭터 폴더를 선택하세요
              </p>
            )}
          </div>

          <div className="flex gap-2 pt-2">
            <Button variant="outline" onClick={onCancel} className="flex-1">
              취소
            </Button>
            <Button
              onClick={handleConfirm}
              disabled={!selectedPath || !fileName.trim()}
              className="flex-1"
            >
              <Download className="size-4 mr-2" />
              다운로드
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
