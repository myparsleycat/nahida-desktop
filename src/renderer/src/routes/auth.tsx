import { Center } from "@renderer/components/common";
import { Titlebar } from "@renderer/components/titlebar";
import { Button } from "@renderer/components/ui/button";
import { Logger } from "@renderer/lib/logger";
import { createFileRoute } from "@tanstack/react-router";
import { Loader2Icon, LogInIcon } from "lucide-react";
import { useState } from "react";

export const Route = createFileRoute("/auth")({
  component: RouteComponent,
});

function RouteComponent() {
  const [loading, setLoading] = useState(false);
  const [showStopBtn, setShowStopBtn] = useState(false);

  const handleLogin = async () => {
    setLoading(true);
    setShowStopBtn(false);

    const timer = setTimeout(() => {
      setShowStopBtn(true);
    }, 5000);

    try {
      await window.api.invoke("auth:startLogin");
    } catch (err) {
      await window.api.invoke("util:showModal", {
        type: "error",
        title: "로그인 에러",
        message: (err as Error).message,
      });
      Logger.error(err as Error, "Route:Auth:handleLogin");
    } finally {
      clearTimeout(timer);
      setLoading(false);
    }
  };

  const handleStop = async () => {
    setLoading(false);
    setShowStopBtn(false);
  };

  return (
    <div className="flex flex-col h-full">
      <Titlebar title={{ text: "로그인", position: "center" }} />

      <Center>
        <div className="flex flex-col gap-2 items-center space-y-4">
          {loading ? (
            <Loader2Icon className="animate-spin size-12" />
          ) : (
            <LogInIcon className="size-12" />
          )}

          <div className="flex space-x-3">
            <Button onClick={handleLogin} disabled={loading}>
              로그인
            </Button>
            {showStopBtn && <Button onClick={handleStop}>중지</Button>}
          </div>
        </div>
      </Center>
    </div>
  );
}
