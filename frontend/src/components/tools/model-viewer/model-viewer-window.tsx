import { Shell } from "@bindings/platform";
import { Tools } from "@bindings/tools";
import { Button } from "@renderer/components/ui/button";
import { Logger } from "@renderer/lib/logger";
import { toErrorMessage } from "@shared/utils";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import type { ModelViewerDialogSource } from "./model-viewer-dialog-types";

import { normalizeModelViewerTransport } from "./model-viewer-transport";
import { ModelViewerWorkspace } from "./model-viewer-workspace";

export function ModelViewerWindow({ path }: { path: string }) {
  const [attempt, setAttempt] = useState(0);
  return (
    <ModelViewerWindowContent
      key={`${path}\0${attempt}`}
      path={path}
      onRetry={() => setAttempt((value) => value + 1)}
    />
  );
}

function ModelViewerWindowContent({ path, onRetry }: { path: string; onRetry: () => void }) {
  const { t } = useTranslation();
  const [source, setSource] = useState<ModelViewerDialogSource | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [previewPath, setPreviewPath] = useState<string>();
  const [doubleSidedEnabled, setDoubleSidedEnabled] = useState(true);

  useEffect(() => {
    let disposed = false;
    let sessionId: string | undefined;
    const cleanup = () => {
      if (!sessionId) return;
      const id = sessionId;
      sessionId = undefined;
      void Tools.CleanupModelViewer(id).catch((reason: unknown) =>
        Logger.capture("model-viewer:cleanup", reason),
      );
    };
    if (!path.trim()) {
      return;
    }
    const load = Tools.LoadModViewer(path);
    void load
      .then((result) => {
        sessionId = result.memorySessionId;
        if (disposed) {
          cleanup();
          return;
        }
        const transport = normalizeModelViewerTransport(result);
        setPreviewPath(transport.previewPath);
        setSource({
          mode: "payload",
          transport,
          memorySessionId: result.memorySessionId,
          modPath: path,
          name: result.name,
        });
      })
      .catch((reason: unknown) => {
        cleanup();
        if (!disposed) {
          Logger.capture("model-viewer:load", reason);
          setError(toErrorMessage(reason));
        }
      });
    const unload = () => {
      disposed = true;
      cleanup();
    };
    window.addEventListener("pagehide", unload);
    return () => {
      disposed = true;
      window.removeEventListener("pagehide", unload);
      cleanup();
      // Let late results reach cleanup; cancelling the binding discards their session ID.
      // Native window closure also fences and cleans sessions on the backend.
    };
  }, [path]);

  const loadError = !path.trim() ? t("page.tools.model_viewer.model_data_unavailable") : error;

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 bg-background p-3 text-foreground">
      <header className="flex shrink-0 items-center justify-between gap-3">
        <div className="min-w-0">
          <h1 className="truncate font-semibold">
            {source?.name || t("page.tools.model_viewer.title")}
          </h1>
          <div className="truncate text-xs text-muted-foreground" title={path}>
            {path}
          </div>
        </div>
        {path && (
          <Button
            variant="outline"
            size="sm"
            onClick={() =>
              void Shell.OpenPath(path).catch((reason: unknown) => {
                Logger.capture("model-viewer:open-folder", reason);
                toast.error(toErrorMessage(reason));
              })
            }
          >
            {t("page.tools.model_viewer.open_folder")}
          </Button>
        )}
      </header>
      {source ? (
        <ModelViewerWorkspace
          open
          doubleSidedEnabled={doubleSidedEnabled}
          onDoubleSidedChange={setDoubleSidedEnabled}
          source={source}
          existingPreviewPath={previewPath}
          onPreviewSaved={setPreviewPath}
        />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-6">
          {loadError ? (
            <>
              <p
                role="alert"
                className="max-w-full break-words whitespace-pre-wrap text-destructive"
              >
                {loadError}
              </p>
              <Button onClick={onRetry}>{t("page.tools.model_viewer.retry")}</Button>
            </>
          ) : (
            <p role="status">{t("page.tools.model_viewer.loading")}</p>
          )}
        </div>
      )}
    </div>
  );
}
