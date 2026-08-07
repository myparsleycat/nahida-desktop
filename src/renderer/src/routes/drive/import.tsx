import { Center } from "@renderer/components/common";
import { Button } from "@renderer/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@renderer/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Label } from "@renderer/components/ui/label";
import { Progress } from "@renderer/components/ui/progress";
import { useAuth } from "@renderer/hooks/use-auth";
import type { DriveCopyProgress } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import { createFileRoute, useRouteContext } from "@tanstack/react-router";
import {
  ArrowLeftIcon,
  CheckIcon,
  ChevronLeftIcon,
  CloudDownloadIcon,
  FolderIcon,
  Loader2Icon,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/drive/import")({
  validateSearch: (search: Record<string, unknown>) => ({
    collectionId: typeof search.collectionId === "string" ? search.collectionId : undefined,
    destinationId: typeof search.destinationId === "string" ? search.destinationId : undefined,
    itemId: typeof search.itemId === "string" ? search.itemId : undefined,
    url: typeof search.url === "string" ? search.url : "",
  }),
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const { session, sessionInitialized, startLogin } = useAuth();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const navigate = Route.useNavigate();
  const search = Route.useSearch();
  const [url, setUrl] = useState(search.url);
  const [password, setPassword] = useState("");
  const [requiresPassword, setRequiresPassword] = useState(false);
  const [destinationId, setDestinationId] = useState(search.destinationId ?? "");
  const [destinationName, setDestinationName] = useState(t("page.drive.import.choose_destination"));
  const [pickerId, setPickerId] = useState(search.destinationId ?? "");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [copyProgress, setCopyProgress] = useState<DriveCopyProgress | null>(null);
  const copyOperationIdRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    setUrl(search.url);
    setPassword("");
    setRequiresPassword(false);
    if (search.destinationId) {
      setDestinationId(search.destinationId);
      setPickerId(search.destinationId);
    } else {
      setDestinationId("");
      setPickerId("");
      setDestinationName(t("page.drive.import.choose_destination"));
    }
  }, [search.destinationId, search.url, t]);

  useEffect(() => {
    if (!session?.drive.rootId || destinationId) return;

    setDestinationId(session.drive.rootId);
    setDestinationName(t("page.drive.title"));
  }, [destinationId, session?.drive.rootId, t]);

  const destinationQuery = useQuery({
    queryKey: ["drive", "import-destination", pickerId],
    enabled: !!pickerId,
    queryFn: async () => await window.api.invoke("drive:get:item", pickerId),
  });

  useEffect(() => {
    if (destinationQuery.data?.content?.id !== destinationId) return;
    setDestinationName(destinationQuery.data.content.name);
  }, [destinationId, destinationQuery.data?.content]);

  useEffect(() => {
    return window.api.on("drive:copy-progress", (progress) => {
      if (progress.operationId !== copyOperationIdRef.current) return;
      setCopyProgress(progress);
    });
  }, []);

  const mutation = useMutation({
    mutationKey: ["drive", "copy-from-url"],
    mutationFn: async () => {
      if (!session) throw new Error("DRIVE_AUTH_REQUIRED");
      const operationId = crypto.randomUUID();
      copyOperationIdRef.current = operationId;

      return await window.api.invoke("drive:fn:copyFromUrl", {
        url: url.trim(),
        password,
        destinationId,
        collectionId: search.collectionId,
        itemId: search.itemId,
        operationId,
      });
    },
  });

  const copyToDrive = useCallback(async () => {
    if (!session) {
      if (sessionInitialized) {
        toast.warning(t("page.drive.import.login_required"));
        await startLogin();
      }
      return;
    }

    if (!destinationId) {
      toast.warning(t("page.drive.import.destination_required"));
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
      await queryClient.invalidateQueries({
        queryKey: ["drive", "drive", destinationId],
        exact: true,
      });
      toast.success(t("page.drive.import.success", { count: result.copied }));
      void navigate({ to: "/drive/drive/$id", params: { id: destinationId } });
    } catch (error) {
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
  }, [
    destinationId,
    mutation,
    navigate,
    password,
    queryClient,
    session,
    sessionInitialized,
    startLogin,
    t,
    url,
  ]);

  const pickerContent = destinationQuery.data?.content;
  const pickerChildren = destinationQuery.data?.children?.filter((item) => item.isDir) ?? [];
  const pickerParentId = destinationQuery.data?.parent?.id ?? pickerContent?.parentId ?? null;

  const openPicker = () => {
    const rootId = session?.drive.rootId;
    if (!rootId) return;
    setPickerId(destinationId || rootId);
    setPickerOpen(true);
  };

  const selectPickerFolder = () => {
    if (!pickerId) return;
    setDestinationId(pickerId);
    setDestinationName(pickerContent?.name ?? pickerId);
    setPickerOpen(false);
  };

  return (
    <>
      <Center>
        <Card className="w-full max-w-xl">
          <CardHeader>
            <CardTitle>{t("page.drive.import.title")}</CardTitle>
            <CardDescription>{t("page.drive.import.description")}</CardDescription>
          </CardHeader>
          <CardContent>
            <form
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault();
                void copyToDrive();
              }}
            >
              <div className="space-y-2">
                <Label htmlFor="drive-import-url">{t("page.drive.import.url_label")}</Label>
                <Input
                  id="drive-import-url"
                  type="url"
                  value={url}
                  onChange={(event) => setUrl(event.target.value)}
                  placeholder={t("page.drive.import.url_placeholder")}
                  autoFocus
                  required
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="drive-import-destination">
                  {t("page.drive.import.destination_label")}
                </Label>
                <div className="flex gap-2">
                  <Input
                    id="drive-import-destination"
                    value={destinationName}
                    readOnly
                    className="min-w-0"
                  />
                  <Button
                    type="button"
                    variant="outline"
                    onClick={openPicker}
                    disabled={!sessionInitialized || !session}
                  >
                    <FolderIcon />
                    {t("page.drive.import.choose_destination")}
                  </Button>
                </div>
              </div>

              {requiresPassword && (
                <div className="space-y-2">
                  <Label htmlFor="drive-import-password">
                    {t("page.drive.import.password_label")}
                  </Label>
                  <Input
                    id="drive-import-password"
                    type="password"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    placeholder={t("page.drive.import.password_placeholder")}
                    autoFocus
                    required
                  />
                </div>
              )}

              {mutation.isPending && copyProgress && (
                <div className="space-y-2 rounded-md border p-3" aria-live="polite">
                  <div className="flex items-center justify-between gap-3 text-sm">
                    <span className="truncate">
                      {copyProgress.itemName ??
                        t(`page.drive.import.progress.${copyProgress.phase}`)}
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

              <div className="flex items-center justify-between gap-3">
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => {
                    if (session?.drive.rootId) {
                      void navigate({
                        to: "/drive/drive/$id",
                        params: { id: session.drive.rootId },
                      });
                      return;
                    }
                    void navigate({ to: "/" });
                  }}
                >
                  <ArrowLeftIcon />
                  {t("g.cancel")}
                </Button>
                <Button type="submit" disabled={mutation.isPending || !sessionInitialized}>
                  {mutation.isPending ? (
                    <Loader2Icon className="animate-spin" />
                  ) : (
                    <CloudDownloadIcon />
                  )}
                  {t("page.drive.import.action")}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      </Center>

      <Dialog open={pickerOpen} onOpenChange={setPickerOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("page.drive.import.choose_destination")}</DialogTitle>
            <DialogDescription>{t("page.drive.import.destination_description")}</DialogDescription>
          </DialogHeader>

          <div className="flex items-center gap-2 rounded-md border px-3 py-2">
            <FolderIcon className="shrink-0 text-yellow-400" />
            <span className="min-w-0 flex-1 truncate">
              {pickerContent?.name ?? t("page.drive.import.loading_folder")}
            </span>
            {pickerId === destinationId && <CheckIcon className="shrink-0 text-primary" />}
          </div>

          <div className="max-h-72 overflow-y-auto rounded-md border">
            {destinationQuery.isError ? (
              <p className="p-6 text-center text-sm text-destructive">
                {t("page.drive.import.destination_load_failed")}
              </p>
            ) : destinationQuery.isFetching ? (
              <div className="flex items-center justify-center p-6">
                <Loader2Icon className="animate-spin" />
              </div>
            ) : pickerChildren.length > 0 ? (
              pickerChildren.map((item) => (
                <button
                  className="flex w-full items-center gap-2 border-b px-3 py-2 text-left last:border-b-0 hover:bg-muted"
                  key={item.id}
                  type="button"
                  onClick={() => setPickerId(item.id)}
                >
                  <FolderIcon className="shrink-0 text-yellow-400" />
                  <span className="min-w-0 truncate">{item.name}</span>
                </button>
              ))
            ) : (
              <p className="p-6 text-center text-sm text-muted-foreground">
                {t("page.drive.import.no_subfolders")}
              </p>
            )}
          </div>

          <DialogFooter className="flex-row justify-between sm:justify-between">
            <Button
              type="button"
              variant="ghost"
              disabled={!pickerParentId || destinationQuery.isFetching || destinationQuery.isError}
              onClick={() => pickerParentId && setPickerId(pickerParentId)}
            >
              <ChevronLeftIcon />
              {t("page.drive.import.parent_folder")}
            </Button>
            <Button
              type="button"
              onClick={selectPickerFolder}
              disabled={!pickerId || !destinationQuery.isSuccess}
            >
              <CheckIcon />
              {t("page.drive.import.select_folder")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function getErrorCode(error: unknown) {
  if (!error || typeof error !== "object") return undefined;

  const code = (error as { code?: unknown }).code;
  return typeof code === "string" ? code : undefined;
}
