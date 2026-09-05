import { Shell } from "@bindings/platform";
import { Logger } from "@renderer/lib/logger";
import { getSetting } from "@renderer/lib/settings";
import { isStartPageSessionReady, resolveStartPage } from "@renderer/lib/start-page";
import { useGlobalStore } from "@renderer/store/global";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

export const Route = createFileRoute("/")({
  component: RouteComponent,
});

function RouteComponent() {
  const navi = useNavigate();
  const sessionInitialized = useGlobalStore((state) => state.sessionInitialized);
  const pendingSessionRestore = useGlobalStore((state) => state.pendingSessionRestore);
  const session = useGlobalStore((state) => state.session);
  const hasToken = useGlobalStore((state) => state.hasToken);
  const backendStatus = useGlobalStore((state) => state.backendStatus);

  useEffect(() => {
    if (
      !isStartPageSessionReady({
        sessionInitialized,
        pendingSessionRestore,
        hasSession: !!session,
        hasToken,
        backendStatus,
      })
    ) {
      return;
    }

    let cancelled = false;

    Promise.all([getSetting("general.defaultStartPage"), Shell.GetAppStatus()])
      .then(([page, appStatus]) => {
        if (cancelled) return;
        void navi({
          to: resolveStartPage(page, {
            isLoggedIn: !!session,
            platform: appStatus?.platform as "win32" | undefined,
            sessionRootId: session?.drive.rootId,
          }),
        });
      })
      .catch((error) => {
        if (cancelled) return;
        Logger.capture("routes/index.tsx", "Failed to resolve startup navigation", error);
        void navi({
          to: resolveStartPage(undefined, {
            isLoggedIn: false,
          }),
        });
      });

    return () => {
      cancelled = true;
    };
  }, [navi, sessionInitialized, pendingSessionRestore, session, hasToken, backendStatus]);

  return <div className="flex min-h-screen flex-col" />;
}
