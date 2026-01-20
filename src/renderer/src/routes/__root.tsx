import { RootProvider } from "@renderer/components/root-provider";
import { Sidebar } from "@renderer/components/sidebar";
import { Titlebar } from "@renderer/components/titlebar";
import { Toaster } from "@renderer/components/ui/sonner";
import { useGlobalEvents } from "@renderer/hooks/useGlobalEvents";
import { DownloadModeDialog } from "@renderer/components/download-mode-dialog";
import { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet, useLocation } from "@tanstack/react-router";
import { useState, useCallback } from "react";

function RootComponent() {
  const location = useLocation();
  const [downloadModeData, setDownloadModeData] = useState<{
    downloadId: string;
    suggestedName?: string;
  } | null>(null);

  const handleDownloadModeSelect = useCallback(
    (data: { downloadId: string; suggestedName?: string }) => {
      setDownloadModeData(data);
    },
    [],
  );

  useGlobalEvents(handleDownloadModeSelect);

  const noSidebarPath = ["/setting", "/auth"];
  const isNoSidebar = noSidebarPath.some((path) => location.pathname.startsWith(path));

  return (
    <>
      <div className="h-7" />

      <Toaster position="bottom-right" richColors />

      {downloadModeData && (
        <DownloadModeDialog
          open={!!downloadModeData}
          onOpenChange={(open) => !open && setDownloadModeData(null)}
          downloadId={downloadModeData.downloadId}
          suggestedName={downloadModeData.suggestedName}
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
