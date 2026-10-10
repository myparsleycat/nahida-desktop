import type { TextureResizeResult, UncompressedTexture } from "@bindings/tools/texture";
import { XXMI } from "@bindings/xxmi";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { formatSize, toErrorMessage } from "@shared/utils";
import { Events } from "@wailsio/runtime";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// The dialog stays open while textures is set. Cancelling a compression stops it from starting
// more files; the ones it is already encoding are still written.
export function UncompressedTexturesDialog({
  importer,
  textures,
  onIgnore,
  onCompressed,
  onClose,
}: {
  importer: string | null;
  textures: UncompressedTexture[] | null;
  onIgnore: () => Promise<void> | void;
  onCompressed: (result: TextureResizeResult) => Promise<void> | void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const compression = useRef<{ cancel: () => unknown; cancelled: boolean } | null>(null);

  useEffect(() => {
    return Events.On("xxmi:texture-compress-progress", (event) => {
      const payload: unknown = Array.isArray(event.data) ? event.data[0] : event.data;
      if (!payload || typeof payload !== "object") return;
      const { done, total } = payload as Record<string, unknown>;
      if (typeof done === "number" && typeof total === "number") {
        setProgress((current) => current && { done, total });
      }
    });
  }, []);

  const cancelCompression = () => {
    if (compression.current) {
      compression.current.cancelled = true;
      void compression.current.cancel();
    }
  };

  // Navigating away must not leave a compression that starts the game once it finishes.
  useEffect(() => cancelCompression, []);

  const close = () => {
    cancelCompression();
    onClose();
  };

  const compress = async () => {
    if (!importer || !textures) return;
    const request = XXMI.CompressLaunchTextures(
      importer,
      textures.map((texture) => texture.path),
    );
    const running = { cancel: () => request.cancel(), cancelled: false };
    compression.current = running;
    setProgress({ done: 0, total: textures.length });
    try {
      const result = await request;
      if (!running.cancelled) await onCompressed(result);
    } catch (error) {
      if (!running.cancelled) toast.error(toErrorMessage(error));
    } finally {
      if (compression.current === running) {
        compression.current = null;
        setProgress(null);
      }
    }
  };

  return (
    <Dialog open={textures !== null} onOpenChange={(open) => !open && close()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("page.mod.dialog.uncompressed-textures.title")}</DialogTitle>
          <DialogDescription>
            {t("page.mod.dialog.uncompressed-textures.description", {
              importer,
              count: textures?.length ?? 0,
            })}
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-72 space-y-1 overflow-y-auto">
          {textures?.map((texture) => (
            <div key={texture.path} className="rounded-lg bg-muted/50 px-3 py-2">
              <p className="text-xs break-all">{texture.relativePath}</p>
              <p className="text-xs text-muted-foreground">
                {texture.width}×{texture.height} · {formatSize(texture.fileSize)}
              </p>
            </div>
          ))}
        </div>
        {progress && (
          <p className="text-sm text-muted-foreground" role="status">
            {t("page.mod.dialog.uncompressed-textures.progress", progress)}
          </p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="outline" onClick={close}>
            {t("g.cancel")}
          </Button>
          <Button
            variant="outline"
            disabled={progress !== null}
            onClickPromise={async () => onIgnore()}
          >
            {t("page.mod.dialog.uncompressed-textures.ignore")}
          </Button>
          <Button disabled={progress !== null} onClick={() => void compress()}>
            {t("page.mod.dialog.uncompressed-textures.confirm")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
