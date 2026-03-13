import { PathSelectorDialog } from "@renderer/components/path-selector-dialog";
import { RootProvider } from "@renderer/components/root-provider";
import { Sidebar } from "@renderer/components/sidebar";
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
import { Toaster } from "@renderer/components/ui/sonner";
import { useGlobalEvents } from "@renderer/hooks/use-global-events";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { cn } from "@renderer/lib/utils";
import { useGlobalStore } from "@renderer/store/global";
import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet, useLocation } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

function UpdateAlertDialog() {
  const { t } = useTranslation();
  const open = useGlobalStore((state) => state.shouldPromptForUpdate);
  const setShouldPromptForUpdate = useGlobalStore((state) => state.setShouldPromptForUpdate);
  const isDismissingRef = useRef(false);
  const skipNextDismissRef = useRef(false);

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
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("updater.toast.available.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("updater.toast.available.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
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
  const setUpdateDownloaded = useGlobalStore((state) => state.setUpdateDownloaded);
  const setShouldPromptForUpdate = useGlobalStore((state) => state.setShouldPromptForUpdate);
  const { i18n } = useTranslation();
  const { screenHeight, titlebarStyle } = useTitlebar();

  useEffect(() => {
    window.api.invoke("util:getAppStatus").then((appStatus) => {
      setAppStatus(appStatus);
    });
    window.api.invoke("updater:getStatus").then((status) => {
      setUpdateDownloaded(status.updateDownloaded);
      setShouldPromptForUpdate(status.shouldPromptForUpdate);
    });
    window.api.invoke("setting:general:getLanguage").then((language) => {
      if (language) i18n.changeLanguage(language);
    });

    const removeUpdateListener = window.api.on("updater:update-downloaded", () => {
      setUpdateDownloaded(true);
      setShouldPromptForUpdate(true);
    });

    return () => {
      removeUpdateListener();
    };
  }, [setAppStatus, setUpdateDownloaded, setShouldPromptForUpdate, i18n]);

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
  const shouldShowUpdateDialog = !isNoSidebar;

  return (
    <>
      {titlebarStyle === "modern" && <div className="h-7 shrink-0" />}

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
