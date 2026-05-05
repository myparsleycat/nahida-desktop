import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { getSetting } from "@renderer/lib/settings";
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
      getSetting("general.defaultStartPage"),
      window.api.invoke("auth:getSession"),
      window.api.invoke("util:getAppStatus"),
    ])
      .then(([page, session, appStatus]) => {
        navi({
          to: resolveStartPage(page, {
            isLoggedIn: !!session,
            platform: appStatus?.platform,
            sessionRootId: session?.drive.rootId,
          }),
        });
      })
      .catch((error) => {
        console.error("Failed to resolve startup navigation", error);
        navi({
          to: resolveStartPage(undefined, {
            isLoggedIn: false,
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
