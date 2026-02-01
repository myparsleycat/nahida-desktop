import { Theme, useTheme } from "@renderer/components/theme-provider";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import { Separator } from "@renderer/components/ui/separator";
import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect } from "react";

export const Route = createFileRoute("/setting/gen")({
  component: RouteComponent,
});

function RouteComponent() {
  const { theme, setTheme } = useTheme();

  const [runOnStartup, setRunOnStartup] = useState(false);
  const [checkBackgroundUpdates, setCheckBackgroundUpdates] = useState(true);
  const [autoUpdate, setAutoUpdate] = useState(false);
  const [moveTransferPageWhenStartTransfer, setMoveTransferPageWhenStartTransfer] = useState(false);
  const [powerSaveBlockInTransfer, setPowerSaveBlockInTransfer] = useState(false);
  const [defaultStartPage, setDefaultStartPage] = useState<string>("/mod");

  useEffect(() => {
    window.api.invoke("setting:general:getRunOnStartup").then((val: boolean) => {
      setRunOnStartup(val);
    });

    window.api.invoke("setting:general:getCheckBackgroundUpdates").then((val: boolean) => {
      setCheckBackgroundUpdates(val);
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
        <CardHeader>
          <CardTitle className="text-sm font-medium">애플리케이션 설정</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">로그인할 때 실행</span>
              <p className="text-xs text-muted-foreground">
                시스템이 시작될 때 앱을 자동으로 실행합니다.
              </p>
            </div>
            <Switch
              checked={runOnStartup}
              onCheckedChange={(val) => {
                setRunOnStartup(val);
                window.api.invoke("setting:general:setRunOnStartup", val);
              }}
            />
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div className="space-y-0.5 flex-1">
              <span className="text-sm font-medium">백그라운드 업데이트 확인</span>
              <p className="text-xs text-muted-foreground">
                앱을 사용 중일 때 새로운 업데이트를 자동으로 확인합니다.
              </p>
            </div>
            <div className="flex items-center gap-4">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => window.api.invoke("setting:general:checkUpdate")}
              >
                업데이트 확인
              </Button>
              <Switch
                checked={checkBackgroundUpdates}
                onCheckedChange={(val) => {
                  setCheckBackgroundUpdates(val);
                  window.api.invoke("setting:general:setCheckBackgroundUpdates", val);
                }}
              />
            </div>
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <span className="text-sm font-medium text-muted-foreground">자동 업데이트</span>
              <p className="text-xs text-muted-foreground">
                업데이트를 확인한 후 자동으로 설치합니다 (준비 중).
              </p>
            </div>
            <Switch checked={autoUpdate} onCheckedChange={setAutoUpdate} disabled />
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
            <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
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
          <label className="text-sm font-medium">테마</label>
          <Select value={theme} onValueChange={(v) => setTheme(v as Theme)}>
            <SelectTrigger className="w-full">
              <SelectValue placeholder="테마 선택" />
            </SelectTrigger>
            <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
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
            <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
              <SelectItem value="/transfer">전송 페이지</SelectItem>
              <SelectItem value="/drive/drive/root">내 드라이브</SelectItem>
              <SelectItem value="/drive/share/root">공유 드라이브</SelectItem>
              <SelectItem value="/mod">모드 매니저</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">작업 및 성능</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">전송 시작 시 페이지 이동</span>
              <p className="text-xs text-muted-foreground">
                파일 전송이 시작되면 자동으로 전송 관리 페이지로 이동합니다.
              </p>
            </div>
            <Switch
              checked={moveTransferPageWhenStartTransfer}
              onCheckedChange={(val) => {
                setMoveTransferPageWhenStartTransfer(val);
                window.api.invoke("setting:general:setMoveTransferPageWhenStartTransfer", val);
              }}
            />
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">절전 모드 방지</span>
              <p className="text-xs text-muted-foreground">
                전송 또는 동기화가 진행 중일 때 시스템이 대기 상태로 들어가는 것을 막습니다.
              </p>
            </div>
            <Switch
              checked={powerSaveBlockInTransfer}
              onCheckedChange={(val) => {
                setPowerSaveBlockInTransfer(val);
                window.api.invoke("setting:general:setPowerSaveBlockInTransfer", val);
              }}
            />
          </div>
        </CardContent>
      </Card>
    </main>
  );
}
