import { PathSelectorDialog } from "@renderer/components/path-selector-dialog";
import { RootProvider } from "@renderer/components/root-provider";
import { Sidebar } from "@renderer/components/sidebar";
import { TitlebarActivityBadges } from "@renderer/components/titlebar/titlebar-activity-badges";
import { use4001FixerTitlebarActivity } from "@renderer/components/titlebar/use-4001-fixer-titlebar-activity";
import { useModBisectTitlebarActivity } from "@renderer/components/titlebar/use-mod-bisect-titlebar-activity";
import { useTransferTitlebarActivity } from "@renderer/components/titlebar/use-transfer-titlebar-activity";
import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
import { Button } from "@renderer/components/ui/button";
import { Toaster } from "@renderer/components/ui/sonner";
import { UpdateAlertDialog } from "@renderer/components/update-alert-dialog";
import { DEFAULT_BG } from "@renderer/const";
import { useGlobalEvents } from "@renderer/hooks/use-global-events";
import { useDownloadArchiveExtractPromptHandler } from "@renderer/hooks/use-mod-events";
import { useTitleBarOverlay } from "@renderer/hooks/use-title-bar-overlay";
import { getSetting } from "@renderer/lib/settings";
import { cn } from "@renderer/lib/utils";
import { useGlobalStore } from "@renderer/store/global";
import type { DownloadSource } from "@shared/mod";
import type { QueryClient } from "@tanstack/react-query";
import {
  createRootRouteWithContext,
  ErrorComponentProps,
  Outlet,
  useLocation,
  useRouter,
} from "@tanstack/react-router";
import { ArrowLeftIcon, DownloadIcon, RefreshCwIcon, TriangleAlertIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

function RootComponent() {
  const location = useLocation();
  const setAppStatus = useGlobalStore((state) => state.setAppStatus);
  const updateAvailable = useGlobalStore((state) => state.updateAvailable);
  const updateDownloaded = useGlobalStore((state) => state.updateDownloaded);
  const setUpdateAvailable = useGlobalStore((state) => state.setUpdateAvailable);
  const setUpdateDownloaded = useGlobalStore((state) => state.setUpdateDownloaded);
  const shouldPromptForUpdate = useGlobalStore((state) => state.shouldPromptForUpdate);
  const setShouldPromptForUpdate = useGlobalStore((state) => state.setShouldPromptForUpdate);
  const setUpdaterStatus = useGlobalStore((state) => state.setUpdaterStatus);
  const updaterMode = useGlobalStore((state) => state.updaterMode);
  const updaterDownloading = useGlobalStore((state) => state.updaterDownloading);
  const setTransfers = useGlobalStore((state) => state.setTransfers);
  const { i18n, t } = useTranslation();
  const [isUpdateActionPending, setIsUpdateActionPending] = useState(false);
  useTitleBarOverlay();
  useTransferTitlebarActivity();
  use4001FixerTitlebarActivity();
  useModBisectTitlebarActivity();

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
    suggestedNames?: string[];
    downloadTargetName?: string;
    downloadImporterKey?: string;
    downloadSource: DownloadSource;
  } | null>(null);

  const handlePathSelectorModeSelect = useCallback(
    (data: {
      selectionId: string;
      suggestedName?: string;
      suggestedNames?: string[];
      downloadTargetName?: string;
      downloadImporterKey?: string;
      downloadSource: DownloadSource;
    }) => {
      setPathSelectorData(data);
    },
    [],
  );

  useGlobalEvents(handlePathSelectorModeSelect);

  const noSidebarPath = ["/auth"];
  const isNoSidebar = noSidebarPath.some((path) => location.pathname.startsWith(path));
  const shouldShowUpdateDialog = !isNoSidebar;

  const isDarwin = window.electron.process.platform === "darwin";
  const shouldOfferManualDownload =
    updateAvailable && !updateDownloaded && (shouldPromptForUpdate || updaterMode === "notify");
  const shouldShowUpdateButton = shouldOfferManualDownload || updateDownloaded;

  return (
    <div className="flex h-screen flex-col overflow-hidden">
      <Toaster position="bottom-right" richColors closeButton />

      {shouldShowUpdateDialog && <UpdateAlertDialog />}

      {pathSelectorData && (
        <PathSelectorDialog
          open={!!pathSelectorData}
          onOpenChange={(open) => !open && setPathSelectorData(null)}
          selectionId={pathSelectorData.selectionId}
          suggestedName={pathSelectorData.suggestedName}
          suggestedNames={pathSelectorData.suggestedNames}
          downloadTargetName={pathSelectorData.downloadTargetName}
          downloadImporterKey={pathSelectorData.downloadImporterKey}
          downloadSource={pathSelectorData.downloadSource}
        />
      )}

      <div
        className={cn(
          "titlebar no-drag flex shrink-0 items-center select-none",
          DEFAULT_BG,
          isDarwin ? "pl-21" : "pl-2",
        )}
      >
        <TitlebarActivityBadges />
        <div className="ml-auto flex h-full shrink-0 items-center gap-2 pr-2">
          {shouldShowUpdateButton && (
            <Button
              type="button"
              size="xs"
              variant="ghost"
              className="h-6 px-2 text-[11.5px]"
              isLoading={isUpdateActionPending || updaterDownloading}
              onClick={async () => {
                setIsUpdateActionPending(true);
                try {
                  if (shouldOfferManualDownload) {
                    await window.api.invoke("updater:downloadUpdate");
                  } else {
                    setShouldPromptForUpdate(true);
                  }
                } finally {
                  setIsUpdateActionPending(false);
                }
              }}
            >
              <DownloadIcon />
              {shouldOfferManualDownload
                ? t("updater.titlebar.downloadAction")
                : t("updater.titlebar.action")}
            </Button>
          )}
        </div>
      </div>
      <div className="shrink-0 border-b" />

      <main className="flex min-h-0 flex-1 overflow-hidden">
        <div className="flex w-full flex-row">
          {!isNoSidebar && <Sidebar />}

          <div className={cn("relative min-w-0 flex-1 overflow-hidden", DEFAULT_BG, "border-l")}>
            <Outlet />
          </div>
        </div>
      </main>
    </div>
  );
}

function NotFoundComponent() {
  const location = useLocation();

  return <div>Not Found here is {location.pathname}</div>;
}

function PendingComponent() {
  return <div>Loading...</div>;
}

function ErrorComponent({ error }: ErrorComponentProps) {
  const { t } = useTranslation();
  const router = useRouter();

  return (
    <div className="flex h-screen flex-col bg-transparent">
      <div className="flex min-h-0 flex-1 items-center justify-center p-6">
        <div className="w-full max-w-md space-y-4">
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>{t("page.root.error.title")}</AlertTitle>
            <AlertDescription>{t("page.root.error.description")}</AlertDescription>
          </Alert>

          {error?.message ? (
            <details className="rounded-lg border bg-muted/40 px-3 py-2 text-xs">
              <summary className="cursor-pointer font-medium text-muted-foreground select-none">
                {t("page.root.error.details")}
              </summary>
              <pre className="mt-2 max-h-48 overflow-auto font-mono wrap-break-word whitespace-pre-wrap text-destructive">
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
    </div>
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
