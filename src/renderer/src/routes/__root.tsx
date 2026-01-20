import { RootProvider } from "@renderer/components/root-provider";
import { Sidebar } from "@renderer/components/sidebar";
import { Titlebar } from "@renderer/components/titlebar";
import { Toaster } from "@renderer/components/ui/sonner";
import { useGlobalEvents } from "@renderer/hooks/useGlobalEvents";
import { PathSelectorDialog } from "@renderer/components/path-selector-dialog";
import { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet, useLocation } from "@tanstack/react-router";
import { useState, useCallback } from "react";

function RootComponent() {
  const location = useLocation();

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

  const noSidebarPath = ["/setting", "/auth"];
  const isNoSidebar = noSidebarPath.some((path) => location.pathname.startsWith(path));

  return (
    <>
      <div className="h-7" />

      <Toaster position="bottom-right" richColors />

      {pathSelectorData && (
        <PathSelectorDialog
          open={!!pathSelectorData}
          onOpenChange={(open) => !open && setPathSelectorData(null)}
          selectionId={pathSelectorData.selectionId}
          suggestedName={pathSelectorData.suggestedName}
        />
      )}

      <main className="flex h-[calc(100vh-28px)] w-screen overflow-hidden">
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
