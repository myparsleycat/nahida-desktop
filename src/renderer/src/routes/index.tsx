import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { resolveStartPage } from "@renderer/lib/start-page";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

export const Route = createFileRoute("/")({
  component: RouteComponent,
});

function RouteComponent() {
  const navi = useNavigate();
  const { Titlebar } = useTitlebar();

  useEffect(() => {
    Promise.all([
      window.api.invoke("setting:general:getDefaultStartPage"),
      window.api.invoke("auth:getSession"),
      window.api.invoke("util:getAppStatus"),
    ]).then(([page, session, appStatus]) => {
      navi({
        to: resolveStartPage(page, {
          isLoggedIn: !!session,
          platform: appStatus?.platform,
          sessionRootId: session?.drive.rootId,
        }),
      });
    });
  }, [navi]);

  return (
    <div className="flex flex-col min-h-screen">
      <Titlebar />
    </div>
  );
}
