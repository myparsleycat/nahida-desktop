import { PathSelectorDialog } from "@renderer/components/path-selector-dialog";
import { RootProvider } from "@renderer/components/root-provider";
import { Sidebar } from "@renderer/components/sidebar";
import { Titlebar } from "@renderer/components/titlebar";
import { Toaster } from "@renderer/components/ui/sonner";
import { useGlobalEvents } from "@renderer/hooks/use-global-events";
import { cn } from "@renderer/lib/utils";
import { useGlobalStore } from "@renderer/store/global";
import type { QueryClient } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet, useLocation } from "@tanstack/react-router";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

function RootComponent() {
  const location = useLocation();
  const setAppStatus = useGlobalStore((state) => state.setAppStatus);
  const setSession = useGlobalStore((state) => state.setSession);
  const { i18n } = useTranslation();

  const { data: titlebarStyle } = useQuery({
    queryKey: ["settings", "general", "titlebarStyle"],
    queryFn: async () => window.api.invoke("setting:general:getTitlebarStyle"),
  });

  useEffect(() => {
    window.api.invoke("util:getAppStatus").then((appStatus) => {
      setAppStatus(appStatus);
    });
    window.api.invoke("auth:getSession").then((session) => {
      setSession(session);
    });
    window.api.invoke("setting:general:getLanguage").then((language) => {
      if (language) i18n.changeLanguage(language);
    });
  }, [setAppStatus, setSession, i18n]);

  const [pathSelectorData, setPathSelectorData] = useState<{
    selectionId: string;
    suggestedName?: string;
  } | null>(null);

  const handlePathSelectorModeSelect = useCallback(
    (data: { selectionId: string; suggestedName?: string }) => {
      setPathSelectorData(data);
    },
    [],
  );

  useGlobalEvents(handlePathSelectorModeSelect);

  const noSidebarPath = ["/setting", "/auth", "/report"];
  const isNoSidebar = noSidebarPath.some((path) => location.pathname.startsWith(path));

  return (
    <>
      {titlebarStyle !== "native" && <div className="h-7 shrink-0" />}

      <Toaster position="bottom-right" richColors />

      {pathSelectorData && (
        <PathSelectorDialog
          open={!!pathSelectorData}
          onOpenChange={(open) => !open && setPathSelectorData(null)}
          selectionId={pathSelectorData.selectionId}
          suggestedName={pathSelectorData.suggestedName}
        />
      )}

      <main
        className={cn(
          "flex w-screen overflow-hidden",
          titlebarStyle === "native" ? "h-screen" : "h-[calc(100vh-28px)]",
        )}
      >
        <div className="flex flex-row w-full">
          {!isNoSidebar && <Sidebar className="border-b" />}

          <div className="flex-1 min-w-0 h-full relative">
            <Outlet />
          </div>
        </div>
      </main>
    </>
  );
}

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  component: () => {
    return (
      <RootProvider>
        <RootComponent />
      </RootProvider>
    );
  },
  notFoundComponent: () => {
    const location = useLocation();
    return (
      <>
        <Titlebar />
        <div>Not Found here is {location.pathname}</div>
      </>
    );
  },
  pendingComponent: () => {
    return (
      <>
        <Titlebar />
        <div>Loading...</div>
      </>
    );
  },
});
