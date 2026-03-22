import { Button } from "@renderer/components/ui/button";
import { cn } from "@renderer/lib/utils";
import { useGlobalStore } from "@renderer/store/global";
import { useQuery } from "@tanstack/react-query";
import { useLocation } from "@tanstack/react-router";
import { find } from "es-toolkit/compat";
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
  "/setting": { hideMinimize: false, hideMaximize: true },
  "/auth": { hideMinimize: false, hideMaximize: true },
};

export function Titlebar({ title }: TitlebarProps) {
  const location = useLocation();
  const { t } = useTranslation();
  const updateAvailable = useGlobalStore((state) => state.updateAvailable);
  const updateDownloaded = useGlobalStore((state) => state.updateDownloaded);
  const updaterDownloading = useGlobalStore((state) => state.updaterDownloading);
  const [isUpdateActionPending, setIsUpdateActionPending] = useState(false);

  const { data: titlebarStyle } = useQuery({
    queryKey: ["settings", "general", "titlebarStyle"],
    queryFn: async () => window.api.invoke("setting:general:getTitlebarStyle"),
  });

  const configEntry = find(
    Object.entries(WINDOW_CONFIG),
    ([route]) => location.pathname === route || location.pathname.startsWith(`${route}/`),
  );

  const { hideMinimize, hideMaximize } = configEntry?.[1] || {};
  const shouldShowUpdateButton = (updateAvailable || updateDownloaded) && !configEntry;
  const isDownloadAction = updateAvailable && !updateDownloaded;

  if (titlebarStyle === "native") return null;

  return (
    <div className="titlebar fixed top-0 left-0 right-0 h-7 bg-background flex items-center select-none z-9999 border-b">
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
            className="h-5 px-2 text-[10px] mr-4"
            isLoading={isUpdateActionPending || updaterDownloading}
            onClick={async () => {
              setIsUpdateActionPending(true);
              try {
                if (isDownloadAction) {
                  await window.api.invoke("updater:downloadUpdate");
                } else {
                  await window.api.invoke("updater:installUpdate");
                }
              } finally {
                setIsUpdateActionPending(false);
              }
            }}
          >
            <DownloadIcon />
            {isDownloadAction
              ? t("updater.titlebar.downloadAction")
              : t("updater.titlebar.action")}
          </Button>
        )}

        {!hideMinimize && (
          <button
            type="button"
            className="flex justify-center items-center px-3 hover:bg-muted duration-150 min h-full"
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
            className="flex justify-center items-center px-3 hover:bg-muted duration-150 max h-full"
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
          className="flex justify-center items-center px-3 hover:bg-red-500 duration-150 close h-full"
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
