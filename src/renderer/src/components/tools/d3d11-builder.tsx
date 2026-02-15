import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { Loader2Icon } from "lucide-react";
import { useEffect, useState } from "react";

export function D3D11Builder() {
  const [isRunning, setIsRunning] = useState(false);
  const [progress, setProgress] = useState("");
  const [gimiPath, setGimiPath] = useState("");

  useEffect(() => {
    window.api.invoke("tools:getGIMIPath").then((path) => {
      setGimiPath(path || "");
    });

    return window.api.on("tools:progress", (message) => {
      setProgress(message);
    });
  }, []);

  const handleBuild = async () => {
    if (isRunning) return;
    setIsRunning(true);
    setProgress("Initializing...");
    try {
      const result = await window.api.invoke("tools:buildNewD3DDLL", {
        gimiPath,
      });
      if (!result) {
        return;
      }
      setProgress("빌드 성공");
    } catch (e) {
      console.error(e);
      setProgress(`Error: ${e}`);
    } finally {
      setIsRunning(false);
    }
  };

  return (
    <div className="space-y-3 w-full">
      <div className="flex flex-col text-sm">
        <span>d3d11.dll 빌드</span>
        <span>새로운 d3d11.dll을 빌드합니다</span>
      </div>

      <div className="flex flex-row gap-2">
        <Input
          value={gimiPath}
          onChange={(e) => {
            setGimiPath(e.target.value);
            window.api.invoke("tools:saveGIMIPath", e.target.value);
          }}
          placeholder="GIMI 경로 입력 (비워둘 경우 %appdata%\XXMI Launcher\GIMI 를 사용)"
        />
        <Button variant="outline" size="sm" disabled={isRunning} onClick={handleBuild}>
          {isRunning && <Loader2Icon className="mr-2 animate-spin" />}
          빌드
        </Button>
      </div>

      {progress && <div className="mt-2 text-sm text-muted-foreground">{progress}</div>}
    </div>
  );
}
