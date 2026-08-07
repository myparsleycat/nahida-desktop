import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { Label } from "@renderer/components/ui/label";
import { Progress } from "@renderer/components/ui/progress";
import { useAuth } from "@renderer/hooks/use-auth";
import { useViewStore } from "@renderer/store/drive";
import type { DriveCopyProgress } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CloudDownloadIcon, FolderIcon, Loader2Icon, XIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function DriveImportOverlay({ destinationId }: { destinationId: string }) {
  const { t } = useTranslation();
  const { session, sessionInitialized, startLogin } = useAuth();
  const queryClient = useQueryClient();

  const importOverlay = useViewStore((s) => s.importOverlay);
  const setImportOverlay = useViewStore((s) => s.setImportOverlay);

  const [url, setUrl] = useState("");
  const [password, setPassword] = useState("");
  const [requiresPassword, setRequiresPassword] = useState(false);
  const [copyProgress, setCopyProgress] = useState<DriveCopyProgress | null>(null);
  const copyOperationIdRef = useRef<string | undefined>(undefined);
  const isPendingRef = useRef(false);

  const destinationQuery = useQuery({
    queryKey: ["drive", "import-destination", destinationId],
    enabled: !!importOverlay && !!destinationId,
    queryFn: async () => await window.api.invoke("drive:get:item", destinationId),
  });

  useEffect(() => {
    if (!importOverlay) return;
    setUrl(importOverlay.url);
    setPassword("");
    setRequiresPassword(false);
    setCopyProgress(null);
  }, [importOverlay]);

  useEffect(() => {
    if (!importOverlay) return;
    return window.api.on("drive:copy-progress", (progress) => {
      if (progress.operationId !== copyOperationIdRef.current) return;
      setCopyProgress(progress);
    });
  }, [importOverlay]);

  useEffect(() => {
    return () => {
      const operationId = copyOperationIdRef.current;
      if (operationId && isPendingRef.current) {
        void window.api.invoke("drive:fn:cancelCopyFromUrl", operationId).catch(() => {});
      }
      setImportOverlay(null);
    };
  }, [setImportOverlay]);

  const mutation = useMutation({
    mutationKey: ["drive", "copy-from-url"],
    mutationFn: async () => {
      if (!session) throw new Error("DRIVE_AUTH_REQUIRED");
      const operationId = crypto.randomUUID();
      copyOperationIdRef.current = operationId;
      isPendingRef.current = true;

      return await window.api.invoke("drive:fn:copyFromUrl", {
        url: url.trim(),
        password,
        destinationId,
        operationId,
      });
    },
  });

  const handleConfirm = async () => {
    if (!session) {
      if (sessionInitialized) {
        toast.warning(t("page.drive.import.login_required"));
        await startLogin();
      }
      return;
    }

    if (!url.trim()) {
      toast.warning(t("page.drive.import.url_required"));
      return;
    }

    setRequiresPassword(false);
    setCopyProgress(null);
    try {
      const result = await mutation.mutateAsync();
      isPendingRef.current = false;
      await queryClient.invalidateQueries({
        queryKey: ["drive", "drive", destinationId],
        exact: true,
      });
      toast.success(t("page.drive.import.success", { count: result.copied }));
      setImportOverlay(null);
    } catch (error) {
      isPendingRef.current = false;
      const code = getErrorCode(error);
      const message = toErrorMessage(error);
      if (code === "DRIVE_LINK_PASSWORD_REQUIRED") {
        setRequiresPassword(true);
        toast.warning(t("page.drive.import.password_required"));
        return;
      }
      if (code === "DRIVE_LINK_INVALID_PASSWORD") {
        setRequiresPassword(true);
        toast.error(t("page.drive.import.invalid_password"));
        return;
      }
      if (message.includes("DRIVE_COPY_CANCELED")) return;
      toast.error(t("page.drive.import.failed"), { description: message });
    }
  };

  const handleCancel = async () => {
    const operationId = copyOperationIdRef.current;
    if (operationId && isPendingRef.current) {
      try {
        await window.api.invoke("drive:fn:cancelCopyFromUrl", operationId);
      } catch {
        // ignore cancel errors
      }
    }
    setImportOverlay(null);
  };

  if (!importOverlay) return null;

  return (
    <div
      className="absolute inset-0 z-50 flex items-center justify-center bg-black/20"
      onClick={(e) => {
        e.stopPropagation();
      }}
    >
      <div
        className="mx-4 w-full max-w-md rounded-lg bg-background/75 p-4 shadow-lg outline backdrop-blur-lg"
        onClick={(e) => {
          e.stopPropagation();
        }}
      >
        <div className="space-y-4">
          <div className="flex items-start justify-between">
            <div>
              <h3 className="text-lg font-semibold">{t("page.drive.import.title")}</h3>
              <p className="mt-1 text-sm text-muted-foreground">
                {t("page.drive.import.description")}
              </p>
            </div>
            <Button variant="ghost" size="icon" className="size-7 shrink-0" onClick={handleCancel}>
              <XIcon className="size-4" />
            </Button>
          </div>

          <div className="space-y-1">
            <Label htmlFor="drive-import-url">{t("page.drive.import.url_label")}</Label>
            <Input
              id="drive-import-url"
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={t("page.drive.import.url_placeholder")}
              autoFocus
              className="w-full"
            />
          </div>

          <div className="space-y-1">
            <p className="text-sm font-medium">{t("page.drive.import.destination_label")}</p>
            <div className="flex items-center gap-2">
              <FolderIcon className="size-4 shrink-0 text-yellow-400" />
              <Input
                value={
                  destinationQuery.data?.content?.name ?? t("page.drive.import.loading_folder")
                }
                className="w-full"
                hideFocusRing
                transparentBackground
                readOnly
              />
            </div>
          </div>

          {requiresPassword && (
            <div className="space-y-1">
              <Label htmlFor="drive-import-password">{t("page.drive.import.password_label")}</Label>
              <Input
                id="drive-import-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={t("page.drive.import.password_placeholder")}
                autoFocus
                className="w-full"
              />
            </div>
          )}

          {mutation.isPending && copyProgress && (
            <div className="space-y-2 rounded-md border p-3" aria-live="polite">
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="truncate">
                  {copyProgress.itemName ?? t(`page.drive.import.progress.${copyProgress.phase}`)}
                </span>
                <span className="shrink-0 text-muted-foreground">
                  {copyProgress.current}/{copyProgress.total}
                </span>
              </div>
              <Progress
                value={
                  copyProgress.phase === "downloading"
                    ? null
                    : copyProgress.total > 0
                      ? (copyProgress.current / copyProgress.total) * 100
                      : null
                }
              />
              <p className="text-xs text-muted-foreground">
                {t(`page.drive.import.progress.${copyProgress.phase}`)}
              </p>
            </div>
          )}

          <div className="flex gap-2 pt-2">
            <Button variant="outline" onClick={handleCancel} className="flex-1">
              {t("g.cancel")}
            </Button>
            <Button
              onClick={handleConfirm}
              disabled={mutation.isPending || !sessionInitialized || !url.trim()}
              className="flex-1"
            >
              {mutation.isPending ? (
                <Loader2Icon className="mr-2 size-4 animate-spin" />
              ) : (
                <CloudDownloadIcon className="mr-2 size-4" />
              )}
              {t("page.drive.import.action")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function getErrorCode(error: unknown) {
  if (!error || typeof error !== "object") return undefined;

  const code = (error as { code?: unknown }).code;
  return typeof code === "string" ? code : undefined;
}
