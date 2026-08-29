import { Auth } from "@bindings/auth";
import { Dialog } from "@bindings/platform";
import { Center } from "@renderer/components/common";
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
      await Auth.StartLogin();
    } catch (err) {
      await Dialog.ShowModal({
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
    <div className="flex h-full flex-col">
      <Center>
        <div className="flex flex-col items-center gap-2 space-y-4">
          {loading ? (
            <Loader2Icon className="size-12 animate-spin" />
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
