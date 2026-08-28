import { cn } from "@renderer/lib/utils";
import { Events, Window as WailsWindow } from "@wailsio/runtime";
import { CopyIcon, MinusIcon, SquareIcon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

const controlClass =
  "flex h-full w-[46px] items-center justify-center text-foreground/80 outline-none";

export function TitlebarWindowControls() {
  const { t } = useTranslation();
  const [maximised, setMaximised] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const sync = () => {
      void WailsWindow.IsMaximised().then((value) => {
        if (!cancelled) {
          setMaximised(value);
        }
      });
    };
    sync();
    const offMaximise = Events.On(Events.Types.Common.WindowMaximise, sync);
    const offUnMaximise = Events.On(Events.Types.Common.WindowUnMaximise, sync);
    const offRestore = Events.On(Events.Types.Common.WindowRestore, sync);
    return () => {
      cancelled = true;
      offMaximise();
      offUnMaximise();
      offRestore();
    };
  }, []);

  return (
    <div className="flex h-full shrink-0">
      <button
        type="button"
        className={cn(
          controlClass,
          "titlebar-window-control--minimise hover:bg-black/10 dark:hover:bg-white/10",
        )}
        aria-label={t("titlebar.controls.minimise")}
        onClick={() => void WailsWindow.Minimise()}
      >
        <MinusIcon className="size-3.5" />
      </button>
      <button
        type="button"
        className={cn(
          controlClass,
          "titlebar-window-control--maximise hover:bg-black/10 dark:hover:bg-white/10",
        )}
        aria-label={t(maximised ? "titlebar.controls.restore" : "titlebar.controls.maximise")}
        onClick={() => {
          void WailsWindow.ToggleMaximise().then(() =>
            WailsWindow.IsMaximised().then(setMaximised),
          );
        }}
      >
        {maximised ? <CopyIcon className="size-3" /> : <SquareIcon className="size-3" />}
      </button>
      <button
        type="button"
        className={cn(
          controlClass,
          "titlebar-window-control--close hover:bg-[#e81123] hover:text-white",
        )}
        aria-label={t("titlebar.controls.close")}
        onClick={() => void WailsWindow.Close()}
      >
        <XIcon className="size-3.5" />
      </button>
    </div>
  );
}
