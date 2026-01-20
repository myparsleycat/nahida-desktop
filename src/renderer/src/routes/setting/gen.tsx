import { Theme, useTheme } from "@renderer/components/theme-provider";
import { Button } from "@renderer/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@renderer/components/ui/card";
import { Checkbox } from "@renderer/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect } from "react";

export const Route = createFileRoute("/setting/gen")({
  component: RouteComponent,
});

function RouteComponent() {
  const { setTheme } = useTheme();

  const [runOnStartup, setRunOnStartup] = useState(false);
  const [autoUpdate, setAutoUpdate] = useState(false);
  const [moveTransferPageWhenStartTransfer, setMoveTransferPageWhenStartTransfer] = useState(false);
  const [powerSaveBlockInTransfer, setPowerSaveBlockInTransfer] = useState(false);
  const [defaultStartPage, setDefaultStartPage] = useState<string>("/mod");

  useEffect(() => {
    window.api.invoke("setting:general:getRunOnStartup").then((val: boolean) => {
      setRunOnStartup(val);
    });

    window.api
      .invoke("setting:general:getMoveTransferPageWhenStartTransfer")
      .then((val: boolean) => {
        setMoveTransferPageWhenStartTransfer(val);
      });

    window.api.invoke("setting:general:getPowerSaveBlockInTransfer").then((val: boolean) => {
      setPowerSaveBlockInTransfer(val);
    });

    window.api.invoke("setting:general:getDefaultStartPage").then((val: string | null) => {
      setDefaultStartPage(val || "/mod");
    });
  }, []);

  return (
    <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none">
      <Card>
        <CardContent>
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <div className="space-y-2 flex-1">
                <label className="flex items-center gap-3 cursor-pointer">
                  <Checkbox
                    checked={runOnStartup}
                    onCheckedChange={(checked) => {
                      const val = checked as boolean;
                      setRunOnStartup(val);
                      window.api.invoke("setting:general:setRunOnStartup", val);
                    }}
                  />
                  <span className="text-sm">로그인할 때 실행</span>
                </label>
                <label className="flex items-center gap-3 cursor-pointer">
                  <Checkbox
                    checked={autoUpdate}
                    onCheckedChange={(checked) => setAutoUpdate(checked as boolean)}
                    disabled
                  />
                  <span className="text-sm text-muted">자동 업데이트</span>
                </label>
              </div>
              <Button
                className="ml-4"
                variant="secondary"
                size="sm"
                onClick={() => window.api.invoke("setting:general:checkUpdate")}
              >
                업데이트 확인
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="grid grid-cols-3 gap-3">
        <div className="space-y-3">
          <label className="text-sm font-medium">언어</label>
          <Select defaultValue="ko">
            <SelectTrigger className="w-full">
              <SelectValue placeholder="언어 선택" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="ko">한국어</SelectItem>
              <SelectItem value="en" disabled>
                English (Soon)
              </SelectItem>
              <SelectItem value="ja" disabled>
                日本語 (Soon)
              </SelectItem>
              <SelectItem value="zh" disabled>
                中文 (Soon)
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-3">
          <label className="text-sm font-medium">Theme</label>
          <Select defaultValue="system" onValueChange={(v) => setTheme(v as Theme)}>
            <SelectTrigger className="w-full">
              <SelectValue placeholder="테마 선택" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="system">시스템 기본값</SelectItem>
              <SelectItem value="light">밝게</SelectItem>
              <SelectItem value="dark">어둡게</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-3">
          <label className="text-sm font-medium">기본 시작 페이지</label>
          <Select
            value={defaultStartPage}
            onValueChange={(v) => {
              setDefaultStartPage(v);
              window.api.invoke("setting:general:setDefaultStartPage", v);
            }}
          >
            <SelectTrigger className="w-full">
              <SelectValue placeholder="시작 페이지 선택" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="/transfer">전송 페이지</SelectItem>
              <SelectItem value="/drive/drive/root">내 드라이브</SelectItem>
              <SelectItem value="/drive/share/root">공유 드라이브</SelectItem>
              <SelectItem value="/mod">모드 매니저</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <Card>
        <CardContent className="space-y-2">
          <label className="flex items-center gap-3 cursor-pointer">
            <Checkbox
              checked={moveTransferPageWhenStartTransfer}
              onCheckedChange={(checked) => {
                const val = checked as boolean;
                setMoveTransferPageWhenStartTransfer(val);
                window.api.invoke("setting:general:setMoveTransferPageWhenStartTransfer", val);
              }}
            />
            <span className="text-sm">전송이 시작될 때 전송 페이지로 이동합니다.</span>
          </label>

          <label className="flex items-center gap-3 cursor-pointer">
            <Checkbox
              checked={powerSaveBlockInTransfer}
              onCheckedChange={(checked) => {
                const val = checked as boolean;
                setPowerSaveBlockInTransfer(val);
                window.api.invoke("setting:general:setPowerSaveBlockInTransfer", val);
              }}
            />
            <span className="text-sm">
              전송 또는 동기화가 진행 중일 때 컴퓨터가 절전 모드에 들어가는 것을 방지합니다.
            </span>
          </label>
        </CardContent>
      </Card>

      {/* <Card>
        <CardContent>
          <div className="flex items-center gap-4">
            <p className="flex-1">만약 어떤 문제에 부딪혔다면 저에게 알려주세요.</p>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => window.api.invoke("window:openReport")}
            >
              문제 신고
            </Button>
          </div>
        </CardContent>
      </Card> */}
    </main>
  );
}
