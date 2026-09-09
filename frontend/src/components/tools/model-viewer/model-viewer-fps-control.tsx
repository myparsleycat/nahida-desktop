import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@renderer/components/ui/popover";
import { useState } from "react";
import { useTranslation } from "react-i18next";

const MIN_FPS = 1;
const MAX_FPS = 1000;

function clampFps(value: number) {
  if (!Number.isFinite(value)) return MIN_FPS;
  return Math.min(Math.max(Math.round(value), MIN_FPS), MAX_FPS);
}

export function ModelViewerFpsControl({
  fps,
  defaultFps,
  onFpsChange,
}: {
  fps: number;
  defaultFps: number;
  onFpsChange: (fps: number) => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(() => String(fps));

  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen);
    if (nextOpen) {
      setDraft(String(fps));
    }
  };

  const commit = () => {
    const parsed = Number(draft);
    if (draft.trim() !== "" && Number.isFinite(parsed)) {
      onFpsChange(clampFps(parsed));
    }
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        title={t("page.tools.model_viewer.fps_control")}
        render={
          <button
            type="button"
            className="cursor-pointer underline decoration-dotted underline-offset-2 outline-hidden transition-colors hover:text-foreground"
          />
        }
      >
        {fps} FPS
      </PopoverTrigger>
      <PopoverContent align="start" className="w-44 gap-2">
        <form
          className="flex items-center gap-1.5"
          onSubmit={(event) => {
            event.preventDefault();
            commit();
          }}
        >
          <Input
            type="number"
            min={MIN_FPS}
            max={MAX_FPS}
            step={1}
            value={draft}
            onChange={(event) => setDraft(event.currentTarget.value)}
            onFocus={(event) => event.currentTarget.select()}
            aria-label={t("page.tools.model_viewer.fps_control")}
            className="flex-1"
          />
          <Button type="submit" size="sm">
            {t("page.tools.model_viewer.fps_apply")}
          </Button>
        </form>
        <div className="text-xs text-muted-foreground">
          {t("page.tools.model_viewer.fps_default", { fps: defaultFps })}
        </div>
      </PopoverContent>
    </Popover>
  );
}
