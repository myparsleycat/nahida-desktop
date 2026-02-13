import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import type { XXMIData } from "@renderer/routes/setting/xxmi";
import { InfoIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

export function XXMIPath({ xxmiData, refetch }: { xxmiData?: XXMIData; refetch: () => void }) {
  const [showAutoSearchAlert, setShowAutoSearchAlert] = useState(false);
  const [xxmiPath, setXXMIPath] = useState("");

  useEffect(() => {
    setXXMIPath(xxmiData?.xxmiPath || "");
  }, [xxmiData]);

  const saveXXMIPath = async () => {
    try {
      await window.api.invoke("xxmi:saveXXMIPath", xxmiPath);
      toast.success("XXMI 경로가 저장되었습니다");
      setShowAutoSearchAlert(false);
      refetch();
    } catch (rawErr) {
      const err = (rawErr as Error).message;

      if (err.includes("XXMI Launcher Config.json not found")) {
        toast.warning("해당 경로에서 XXMI Launcher Config.json 파일을 찾을 수 없습니다");
      }
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>XXMI 경로 설정</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 w-full" aria-describedby={undefined}>
        <div className="flex flex-row w-full space-x-2">
          <Input
            value={xxmiPath}
            onChange={(e) => {
              setXXMIPath(e.target.value);
            }}
          />
          <Button
            variant="outline"
            onClickPromise={async () => {
              setShowAutoSearchAlert(false);
              const path = await window.api.invoke("xxmi:findXXMIPath");

              if (!path) {
                toast.error("XXMI 경로를 찾을 수 없습니다");
                return;
              }

              setXXMIPath(path);
              setShowAutoSearchAlert(true);
            }}
          >
            자동 탐색
          </Button>
        </div>
        {showAutoSearchAlert && (
          <Alert>
            <InfoIcon />
            <AlertTitle>알림</AlertTitle>
            <AlertDescription className="text-wrap">
              자동 탐색된 XXMI 런처 경로를 확인하세요. 정상적이지 않다면 직접 경로를 입력해주세요.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
      <CardFooter className="flex justify-end">
        <Button onClickPromise={saveXXMIPath} disabled={!xxmiPath}>
          저장
        </Button>
      </CardFooter>
    </Card>
  );
}
