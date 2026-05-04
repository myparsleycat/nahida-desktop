import { Button } from "@renderer/components/ui/button";
import { useSetting } from "@renderer/hooks/use-settings";
import { cn } from "@renderer/lib/utils";
import { useGlobalStore } from "@renderer/store/global";
import { useLocation } from "@tanstack/react-router";
import { DownloadIcon, MaximizeIcon, MinusIcon, XIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

interface TitlebarProps {
  title?: {
    text: string;
    position?: "left" | "center";
  };
}

const WINDOW_CONFIG: Record<string, { hideMinimize?: boolean; hideMaximize?: boolean }> = {
  "/report": { hideMinimize: true, hideMaximize: true },
  "/auth": { hideMinimize: false, hideMaximize: true },
};

export function Titlebar({ title }: TitlebarProps) {
  const location = useLocation();
  const { t } = useTranslation();
  const updateAvailable = useGlobalStore((state) => state.updateAvailable);
  const updateDownloaded = useGlobalStore((state) => state.updateDownloaded);
  const shouldPromptForUpdate = useGlobalStore((state) => state.shouldPromptForUpdate);
  const setShouldPromptForUpdate = useGlobalStore((state) => state.setShouldPromptForUpdate);
  const updaterMode = useGlobalStore((state) => state.updaterMode);
  const updaterDownloading = useGlobalStore((state) => state.updaterDownloading);
  const [isUpdateActionPending, setIsUpdateActionPending] = useState(false);

  const { data: titlebarStyle } = useSetting("general.titlebarStyle");

  const configEntry = Object.entries(WINDOW_CONFIG).find(
    ([route]) => location.pathname === route || location.pathname.startsWith(`${route}/`),
  );

  const { hideMinimize, hideMaximize } = configEntry?.[1] || {};
  const shouldOfferManualDownload =
    updateAvailable && !updateDownloaded && (shouldPromptForUpdate || updaterMode === "notify");
  const shouldShowUpdateButton = (shouldOfferManualDownload || updateDownloaded) && !configEntry;

  if (titlebarStyle === "native") return null;

  return (
    <div
      className="titlebar fixed top-0 left-0 right-0 h-8 bg-background flex items-center select-none z-9999 border-b"
      onPointerDownCapture={(e) => {
        e.stopPropagation();
      }}
    >
      <div
        className={cn(
          "flex items-center px-2 h-full w-full",
          title?.position === "center"
            ? "absolute inset-0 justify-center pointer-events-none"
            : "justify-start",
        )}
      >
        {title && (
          <p
            className={cn(
              "text-sm text-current",
              title.position === "center" && "pointer-events-auto",
            )}
          >
            {title.text}
          </p>
        )}
      </div>

      <div className="buttons flex h-full ml-auto z-10 items-center">
        {shouldShowUpdateButton && (
          <Button
            type="button"
            size="xs"
            variant="outline"
            className="titlebar-action h-6 px-2 text-[11.5px] mr-4"
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

        {!hideMinimize && (
          <button
            type="button"
            className="titlebar-action flex justify-center items-center px-3 hover:bg-muted duration-150 min h-full"
            tabIndex={-1}
            onClick={() => {
              window.electron.ipcRenderer.send("window-control", "minimize");
            }}
          >
            <MinusIcon className="size-4" />
          </button>
        )}

        {!hideMaximize && (
          <button
            type="button"
            className="titlebar-action flex justify-center items-center px-3 hover:bg-muted duration-150 max h-full"
            tabIndex={-1}
            onClick={() => {
              window.electron.ipcRenderer.send("window-control", "maximize");
            }}
          >
            <MaximizeIcon className="size-4" />
          </button>
        )}

        <button
          type="button"
          className="titlebar-action flex justify-center items-center px-3 hover:bg-red-500 duration-150 close h-full"
          tabIndex={-1}
          onClick={() => {
            window.electron.ipcRenderer.send("window-control", "close");
          }}
        >
          <XIcon className="size-4" />
        </button>
      </div>
    </div>
  );
}
