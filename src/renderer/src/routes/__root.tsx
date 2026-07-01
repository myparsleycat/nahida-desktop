import { PathSelectorDialog } from "@renderer/components/path-selector-dialog";
import { RootProvider } from "@renderer/components/root-provider";
import { Sidebar } from "@renderer/components/sidebar";
import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
import { Button } from "@renderer/components/ui/button";
import { Toaster } from "@renderer/components/ui/sonner";
import { UpdateAlertDialog } from "@renderer/components/update-alert-dialog";
import { DEFAULT_BG } from "@renderer/const";
import { useGlobalEvents } from "@renderer/hooks/use-global-events";
import { useDownloadArchiveExtractPromptHandler } from "@renderer/hooks/use-mod-events";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { getSetting } from "@renderer/lib/settings";
import { cn } from "@renderer/lib/utils";
import { useGlobalStore } from "@renderer/store/global";
import type { QueryClient } from "@tanstack/react-query";
import {
  createRootRouteWithContext,
  ErrorComponentProps,
  Outlet,
  useLocation,
  useRouter,
} from "@tanstack/react-router";
import { ArrowLeftIcon, RefreshCwIcon, TriangleAlertIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

function RootComponent() {
  const location = useLocation();
  const setAppStatus = useGlobalStore((state) => state.setAppStatus);
  const setUpdateAvailable = useGlobalStore((state) => state.setUpdateAvailable);
  const setUpdateDownloaded = useGlobalStore((state) => state.setUpdateDownloaded);
  const setShouldPromptForUpdate = useGlobalStore((state) => state.setShouldPromptForUpdate);
  const setUpdaterStatus = useGlobalStore((state) => state.setUpdaterStatus);
  const setTransfers = useGlobalStore((state) => state.setTransfers);
  const { i18n } = useTranslation();
  const { screenHeight, titlebarStyle } = useTitlebar();

  useDownloadArchiveExtractPromptHandler();

  useEffect(() => {
    const removeStatusListener = window.api.on("updater:status-changed", (status) => {
      setUpdaterStatus(status);
    });

    const removeUpdateAvailableListener = window.api.on("updater:update-available", () => {
      setUpdateAvailable(true);
    });

    const removeUpdateListener = window.api.on("updater:update-downloaded", () => {
      setUpdateAvailable(true);
      setUpdateDownloaded(true);
      setShouldPromptForUpdate(true);
    });

    const syncUpdaterStatus = () => {
      void window.api.invoke("updater:getStatus").then((status) => {
        setUpdaterStatus(status);
      });
    };

    const removeWindowFocusListener = window.api.on("window:focus", () => {
      syncUpdaterStatus();
    });

    const removeTransferListener = window.api.on("transfer:update", (updatedTransfers) => {
      setTransfers(updatedTransfers);
    });

    void window.api.invoke("util:getAppStatus").then((appStatus) => {
      setAppStatus(appStatus);
    });
    syncUpdaterStatus();
    void window.api.invoke("transfer:list").then(setTransfers);
    getSetting("general.language")
      .then((language) => {
        if (language) {
          void i18n.changeLanguage(language).catch((error) => {
            console.error("Failed to change language from getSetting(general.language)", error);
          });
        }
      })
      .catch((error) => {
        console.error("Failed to read getSetting(general.language)", error);
      });

    return () => {
      removeStatusListener();
      removeUpdateAvailableListener();
      removeUpdateListener();
      removeWindowFocusListener();
      removeTransferListener();
    };
  }, [
    setAppStatus,
    setUpdateAvailable,
    setUpdateDownloaded,
    setShouldPromptForUpdate,
    setUpdaterStatus,
    setTransfers,
    i18n,
  ]);

  const [pathSelectorData, setPathSelectorData] = useState<{
    selectionId: string;
    suggestedName?: string;
    downloadTargetName?: string;
    downloadImporterKey?: string;
  } | null>(null);

  const handlePathSelectorModeSelect = useCallback(
    (data: {
      selectionId: string;
      suggestedName?: string;
      downloadTargetName?: string;
      downloadImporterKey?: string;
    }) => {
      setPathSelectorData(data);
    },
    [],
  );

  useGlobalEvents(handlePathSelectorModeSelect);

  const noSidebarPath = ["/auth"];
  const isNoSidebar = noSidebarPath.some((path) => location.pathname.startsWith(path));
  const shouldShowUpdateDialog = !isNoSidebar;

  return (
    <>
      {titlebarStyle === "modern" && <div className="h-8 shrink-0" />}

      <Toaster position="bottom-right" richColors />

      {shouldShowUpdateDialog && <UpdateAlertDialog />}

      {pathSelectorData && (
        <PathSelectorDialog
          open={!!pathSelectorData}
          onOpenChange={(open) => !open && setPathSelectorData(null)}
          selectionId={pathSelectorData.selectionId}
          suggestedName={pathSelectorData.suggestedName}
          downloadTargetName={pathSelectorData.downloadTargetName}
          downloadImporterKey={pathSelectorData.downloadImporterKey}
        />
      )}

      <main className={cn("flex w-screen overflow-hidden", screenHeight)}>
        <div className="flex flex-row w-full">
          {!isNoSidebar && <Sidebar />}

          <div
            className={cn(
              "flex-1 min-w-0 h-full relative overflow-hidden",
              DEFAULT_BG,
              titlebarStyle === "modern" ? "border-t border-l rounded-tl-lg" : "border-l",
            )}
          >
            <Outlet />
          </div>
        </div>
      </main>
    </>
  );
}

function NotFoundComponent() {
  const location = useLocation();
  const { Titlebar } = useTitlebar();

  return (
    <>
      <Titlebar />
      <div>Not Found here is {location.pathname}</div>
    </>
  );
}

function PendingComponent() {
  const { Titlebar } = useTitlebar();

  return (
    <>
      <Titlebar />
      <div>Loading...</div>
    </>
  );
}

function ErrorComponent({ error }: ErrorComponentProps) {
  const { t } = useTranslation();
  const { Titlebar, screenHeight, titlebarStyle } = useTitlebar();
  const router = useRouter();

  return (
    <>
      <Titlebar />
      {titlebarStyle === "modern" && <div className="h-8 shrink-0" />}
      <div className={cn("flex w-screen items-center justify-center p-6", screenHeight)}>
        <div className="w-full max-w-md space-y-4">
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>{t("page.root.error.title")}</AlertTitle>
            <AlertDescription>{t("page.root.error.description")}</AlertDescription>
          </Alert>

          {error?.message ? (
            <details className="rounded-lg border bg-muted/40 px-3 py-2 text-xs">
              <summary className="cursor-pointer select-none font-medium text-muted-foreground">
                {t("page.root.error.details")}
              </summary>
              <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap wrap-break-word font-mono text-destructive">
                {error.message}
              </pre>
            </details>
          ) : null}

          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => router.history.back()}>
              <ArrowLeftIcon />
              {t("page.root.error.back")}
            </Button>
            <Button variant="outline" onClickPromise={() => router.invalidate()}>
              <RefreshCwIcon />
              {t("page.root.error.refresh")}
            </Button>
          </div>
        </div>
      </div>
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
  notFoundComponent: NotFoundComponent,
  pendingComponent: PendingComponent,
  errorComponent: ErrorComponent,
});
