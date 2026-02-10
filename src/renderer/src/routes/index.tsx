import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

export const Route = createFileRoute("/")({
  component: RouteComponent,
});

function RouteComponent() {
  const navi = useNavigate();

  useEffect(() => {
    window.api.invoke("setting:general:getDefaultStartPage").then((page: string | null) => {
      const targetPage = page || "/mod";
      navi({ to: targetPage });
    });
  }, [navi]);

  return (
    <div className="flex flex-col min-h-screen">
      <Titlebar />
    </div>
  );
}
