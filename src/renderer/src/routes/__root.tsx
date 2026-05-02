import { PathSelectorDialog } from "@renderer/components/path-selector-dialog";
import { RootProvider } from "@renderer/components/root-provider";
import { Sidebar } from "@renderer/components/sidebar";
import { Button } from "@renderer/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Toaster } from "@renderer/components/ui/sonner";
import { useGlobalEvents } from "@renderer/hooks/use-global-events";
import { useDownloadArchiveExtractPromptHandler } from "@renderer/hooks/use-mod-events";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { cn } from "@renderer/lib/utils";
import { useGlobalStore } from "@renderer/store/global";
import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet, useLocation } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

function UpdateAlertDialog() {
  const { t } = useTranslation();
  const appStatus = useGlobalStore((state) => state.appStatus);
  const open = useGlobalStore((state) => state.shouldPromptForUpdate);
  const releaseVersion = useGlobalStore((state) => state.releaseVersion);
  const releaseNotes = useGlobalStore((state) => state.releaseNotes);
  const setShouldPromptForUpdate = useGlobalStore((state) => state.setShouldPromptForUpdate);
  const isDismissingRef = useRef(false);
  const skipNextDismissRef = useRef(false);
  const [showOriginalReleaseNotes, setShowOriginalReleaseNotes] = useState(false);
  const versionRangeText =
    appStatus?.version && releaseVersion ? ` (${appStatus.version} → ${releaseVersion})` : "";
  const hasTranslatedReleaseNotes = !!(releaseNotes?.translated && releaseNotes?.original);
  const displayedReleaseNotesText =
    hasTranslatedReleaseNotes && !showOriginalReleaseNotes
      ? releaseNotes.translated
      : releaseNotes?.original ?? releaseNotes?.translated ?? null;

  useEffect(() => {
    setShowOriginalReleaseNotes(false);
  }, [releaseNotes?.original, releaseNotes?.translated, releaseNotes?.translatedLanguage]);

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setShouldPromptForUpdate(true);
      return;
    }

    setShouldPromptForUpdate(false);

    if (skipNextDismissRef.current) {
      skipNextDismissRef.current = false;
      return;
    }

    if (isDismissingRef.current) {
      return;
    }

    isDismissingRef.current = true;
    window.api.invoke("updater:dismissUpdateDialog").finally(() => {
      isDismissingRef.current = false;
    });
  };

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent className="min-w-xl">
        <AlertDialogHeader>
          <AlertDialogTitle>{t("updater.toast.available.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("updater.toast.available.description")}
            <br />
            {versionRangeText}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {displayedReleaseNotesText && (
          <section className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <h3 className="text-sm font-medium">
                {t("updater.toast.available.releaseNotesTitle")}
              </h3>
              {hasTranslatedReleaseNotes && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setShowOriginalReleaseNotes((current) => !current)}
                >
                  {showOriginalReleaseNotes
                    ? t("updater.toast.available.showTranslation")
                    : t("updater.toast.available.showOriginal")}
                </Button>
              )}
            </div>
            <ScrollArea className="h-64 rounded-md border">
              <div className="px-4 py-3 text-sm whitespace-pre-wrap wrap-break-word">
                {displayedReleaseNotesText}
              </div>
            </ScrollArea>
          </section>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>{t("g.later")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              skipNextDismissRef.current = true;
              setShouldPromptForUpdate(false);
              window.api.invoke("updater:installUpdate");
            }}
          >
            {t("updater.toast.available.action")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

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
      window.api.invoke("updater:getStatus").then((status) => {
        setUpdaterStatus(status);
      });
    };

    const removeWindowFocusListener = window.api.on("window:focus", () => {
      syncUpdaterStatus();
    });

    const removeTransferListener = window.api.on("transfer:update", (updatedTransfers) => {
      setTransfers(updatedTransfers);
    });

    window.api.invoke("util:getAppStatus").then((appStatus) => {
      setAppStatus(appStatus);
    });
    syncUpdaterStatus();
    window.api.invoke("transfer:list").then(setTransfers);
    window.api.invoke("setting:general:getLanguage").then((language) => {
      if (language) i18n.changeLanguage(language);
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
  } | null>(null);

  const handlePathSelectorModeSelect = useCallback(
    (data: { selectionId: string; suggestedName?: string }) => {
      setPathSelectorData(data);
    },
    [],
  );

  useGlobalEvents(handlePathSelectorModeSelect);

  const noSidebarPath = ["/auth", "/report"];
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
        />
      )}

      <main className={cn("flex w-screen overflow-hidden", screenHeight)}>
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
});
