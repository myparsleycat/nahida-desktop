import { cn } from "@renderer/lib/utils";
import { useLocation } from "@tanstack/react-router";
import { find } from "es-toolkit/compat";
import { MaximizeIcon, MinusIcon, XIcon } from "lucide-react";

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

  const configEntry = find(
    Object.entries(WINDOW_CONFIG),
    ([route]) => location.pathname === route || location.pathname.startsWith(`${route}/`),
  );

  const { hideMinimize, hideMaximize } = configEntry?.[1] || {};

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

      <div className="buttons flex h-full ml-auto z-10">
        {!hideMinimize && (
          <button
            type="button"
            className="flex justify-center items-center px-3 hover:bg-muted duration-150 min"
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
            className="flex justify-center items-center px-3 hover:bg-muted duration-150 max"
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
          className="flex justify-center items-center px-3 hover:bg-red-500 duration-150 close"
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
