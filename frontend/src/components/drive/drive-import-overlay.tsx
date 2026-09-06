import { Drive } from "@bindings/drive";
import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Input } from "@renderer/components/ui/input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@renderer/components/ui/input-group";
import { Label } from "@renderer/components/ui/label";
import { Progress } from "@renderer/components/ui/progress";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useAuth } from "@renderer/hooks/use-auth";
import { cn } from "@renderer/lib/utils";
import { useViewStore } from "@renderer/store/drive";
import type { DriveCopyProgress } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import {
  ArrowLeftIcon,
  CheckIcon,
  CloudDownloadIcon,
  FolderIcon,
  LinkIcon,
  Loader2Icon,
  LockIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { getErrorCode, isNotFoundError } from "./drive-import-errors";
import { ImportFolderTree } from "./import-folder-tree";
import { pruneSubtreeSelection } from "./import-folder-tree-state";
import { useDriveImportSession } from "./use-drive-import-session";

export function DriveImportOverlay({ destinationId }: { destinationId: string }) {
  const importOverlay = useViewStore((s) => s.importOverlay);
  if (!importOverlay) return null;
  return (
    <DriveImportDialog
      key={importOverlay.url}
      importOverlay={importOverlay}
      destinationId={destinationId}
    />
  );
}

function DriveImportDialog({
  importOverlay,
  destinationId,
}: {
  importOverlay: { url: string };
  destinationId: string;
}) {
  const { t } = useTranslation();
  const { session, sessionInitialized, startLogin } = useAuth();
  const queryClient = useQueryClient();

  const setImportOverlay = useViewStore((s) => s.setImportOverlay);

  const {
    url,
    setUrl,
    password,
    setPassword,
    requiresPassword,
    passwordInvalid,
    setPasswordInvalid,
    step,
    resolving,
    sourceInfo,
    selected,
    expanded,
    loadingIds,
    getParentId,
    selectedAncestorIds,
    visibleNodes,
    handleResolve,
    handleExpand,
    handleCollapse,
    handleToggle,
    resetSession,
  } = useDriveImportSession(importOverlay.url);
  const [createCollectionFolders, setCreateCollectionFolders] = useState(true);
  const [treeViewport, setTreeViewport] = useState<HTMLDivElement | null>(null);
  const [copyProgress, setCopyProgress] = useState<DriveCopyProgress | null>(null);
  const [isImporting, setIsImporting] = useState(false);
  const copyOperationIdRef = useRef<string | undefined>(undefined);
  const isPendingRef = useRef(false);
  const isModSource = sourceInfo?.source === "mod";
  const urlInputRef = useRef<HTMLInputElement>(null);
  const passwordInputRef = useRef<HTMLInputElement>(null);

  const destinationQuery = useQuery({
    queryKey: ["drive", "import-destination", destinationId],
    enabled: !!destinationId,
    queryFn: async () => await Drive.GetItem(destinationId),
  });

  useEffect(() => {
    requestAnimationFrame(() => urlInputRef.current?.focus());
  }, []);

  useEffect(() => {
    return Events.On("drive:copy-progress", (event) => {
      const progress = event.data;
      if (progress.operationId !== copyOperationIdRef.current) return;
      setCopyProgress(progress);
    });
  }, []);

  // focus password when required
  useEffect(() => {
    if (requiresPassword) requestAnimationFrame(() => passwordInputRef.current?.focus());
  }, [requiresPassword]);

  const selectedCount = selected.size;
  const canImport = selectedCount > 0 && !isImporting && !resolving;

  const importMutation = useMutation({
    mutationKey: ["drive", "copy-from-url-many"],
    mutationFn: async () => {
      if (!session) throw new Error("DRIVE_AUTH_REQUIRED");
      if (!sourceInfo) throw new Error("SOURCE_NOT_RESOLVED");
      const operationId = crypto.randomUUID();
      copyOperationIdRef.current = operationId;
      isPendingRef.current = true;
      setIsImporting(true);
      setCopyProgress({
        operationId,
        source: sourceInfo.source,
        phase: "preparing",
        current: 0,
        total: selected.size,
      });

      // prune selectedIds: remove descendants where ancestor already selected (already pruned on toggle, but double-check)
      const pruned = pruneSubtreeSelection(selected, getParentId);
      const selectedIds = [...pruned];
      if (selectedIds.length === 0) throw new Error("NO_SELECTION");

      const result = await Drive.CopyFromURL({
        url: url.trim(),
        password: password || undefined,
        destinationId,
        createCollectionFolders,
        operationId,
        selectedIds,
      });
      return result;
    },
  });

  const handleImport = async () => {
    if (!session) {
      if (sessionInitialized) {
        toast.warning(t("page.drive.import.login_required"));
        await startLogin();
      }
      return;
    }
    if (selected.size === 0) {
      toast.warning("가져올 폴더를 선택하세요.");
      return;
    }
    const destinationIdSnapshot = destinationId;
    // 전송 시작 시 overlay를 즉시 닫는다. 실제 복사는 transfer로 승격되어 백그라운드에서 계속되며,
    // 설정 general.moveTransferPageWhenStartTransfer 가 켜져 있으면 main의 transfer.createTransfer
    // 에서 fn:navi 로 /transfer 이동을 트리거한다 (use-global-events.ts:67).
    setImportOverlay(null);

    void importMutation
      .mutateAsync()
      .then(async (result) => {
        isPendingRef.current = false;
        setIsImporting(false);
        await queryClient.invalidateQueries({
          queryKey: ["drive", "drive", destinationIdSnapshot],
          exact: true,
        });
        toast.success(t("page.drive.import.success", { count: result.copied }));
      })
      .catch((error: unknown) => {
        isPendingRef.current = false;
        setIsImporting(false);
        const code = getErrorCode(error);
        const message = toErrorMessage(error);
        if (code === "DRIVE_LINK_PASSWORD_REQUIRED" || code === "DRIVE_MOD_PASSWORD_REQUIRED") {
          toast.warning(t("page.drive.import.password_required"));
          return;
        }
        if (code === "DRIVE_LINK_INVALID_PASSWORD" || code === "DRIVE_MOD_INVALID_PASSWORD") {
          toast.error(t("page.drive.import.invalid_password"));
          return;
        }
        if (isNotFoundError(error)) {
          toast.error("링크가 만료되었거나 존재하지 않습니다.");
          return;
        }
        if (message.includes("DRIVE_COPY_CANCELED") || message.includes("canceled")) return;
        if (message.includes("NO_SELECTION")) {
          toast.warning("선택된 폴더가 없습니다.");
          return;
        }
        toast.error(t("page.drive.import.failed"), { description: message });
      });
  };

  const handleBack = () => {
    if (isImporting) return;
    resetSession();
    setCopyProgress(null);
  };

  const handleCancel = async () => {
    const operationId = copyOperationIdRef.current;
    if (operationId && isPendingRef.current) {
      try {
        await Drive.CancelCopyFromURL(operationId);
      } catch {}
    }
    isPendingRef.current = false;
    setIsImporting(false);
    setImportOverlay(null);
  };

  const isUrlStep = step === 1;
  const isTreeStep = step === 3;

  return (
    <div
      className="absolute inset-0 z-50 flex items-center justify-center bg-black/20 p-4"
      onClick={(e) => e.stopPropagation()}
    >
      <div
        className={cn(
          "flex max-h-[85vh] w-full flex-col overflow-hidden rounded-lg bg-background/80 shadow-lg outline backdrop-blur-lg",
          isTreeStep ? "max-w-5xl" : "max-w-sm",
        )}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b p-2 px-4">
          <h3 className="text-base font-semibold">{t("page.drive.import.title")}</h3>
          <Button variant="ghost" size="icon" className="size-7 shrink-0" onClick={handleCancel}>
            <XIcon className="size-4" />
          </Button>
        </div>

        {isUrlStep ? (
          <div className="flex flex-1 flex-col gap-4 p-4">
            <div className="space-y-1">
              <Label htmlFor="drive-import-url" className="flex items-center gap-1.5">
                <LinkIcon className="size-3" />
                {t("page.drive.import.url_label")}
              </Label>
              <div className="flex gap-2">
                <Input
                  ref={urlInputRef}
                  id="drive-import-url"
                  type="url"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder={t("page.drive.import.url_placeholder")}
                  className="flex-1"
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !resolving) void handleResolve();
                  }}
                />
              </div>
              <p className="text-xs text-muted-foreground">
                https://nahida.live/akasha/link/... 또는 /akasha/mod/... 형식
              </p>
            </div>

            {requiresPassword && (
              <div className="space-y-1">
                <Label htmlFor="drive-import-password" className="flex items-center gap-1.5">
                  <LockIcon className="size-3" />
                  {t("page.drive.import.password_label")}
                </Label>
                <InputGroup>
                  <InputGroupInput
                    ref={passwordInputRef}
                    id="drive-import-password"
                    type="password"
                    value={password}
                    aria-invalid={passwordInvalid || undefined}
                    onChange={(e) => {
                      setPassword(e.target.value);
                      setPasswordInvalid(false);
                    }}
                    placeholder={t("page.drive.import.password_placeholder")}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && !resolving) void handleResolve();
                    }}
                  />
                  <InputGroupAddon align="inline-end">
                    <InputGroupButton
                      size="icon-xs"
                      disabled={!password.trim() || resolving}
                      aria-label={t("page.drive.import.authenticate")}
                      onClick={() => void handleResolve()}
                    >
                      {resolving ? (
                        <Loader2Icon className="size-3.5 animate-spin" />
                      ) : (
                        <CheckIcon className="size-3.5" />
                      )}
                    </InputGroupButton>
                  </InputGroupAddon>
                </InputGroup>
                {passwordInvalid && (
                  <p className="text-xs text-destructive">
                    {t("page.drive.import.invalid_password")}
                  </p>
                )}
                <p className="text-xs text-muted-foreground">비밀번호가 필요한 링크입니다.</p>
              </div>
            )}

            <div className="space-y-1">
              <p className="text-sm font-medium">{t("page.drive.import.destination_label")}</p>
              <div className="flex items-center gap-2">
                <FolderIcon className="size-4 shrink-0 text-yellow-400" />
                <Input
                  value={
                    destinationQuery.data?.content?.name ?? t("page.drive.import.loading_folder")
                  }
                  className="w-full"
                  readOnly
                />
              </div>
            </div>

            {resolving && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2Icon className="size-4 animate-spin" />
                링크 정보를 불러오는 중...
              </div>
            )}
          </div>
        ) : isTreeStep ? (
          <div className="flex min-h-0 flex-1">
            <div className="flex w-[320px] shrink-0 flex-col border-r">
              <div className="border-b px-4 py-3">
                <h4 className="text-sm font-medium">가져오기 설정</h4>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  폴더만 선택 가능합니다. 파일은 비활성화됩니다.
                </p>
              </div>

              <div className="flex-1 space-y-4 p-4">
                <div className="space-y-1">
                  <Label className="text-xs">공유 URL</Label>
                  <Input value={url} readOnly className="text-xs" />
                </div>

                <div className="space-y-1">
                  <p className="text-xs font-medium">{t("page.drive.import.destination_label")}</p>
                  <div className="flex items-center gap-2">
                    <FolderIcon className="size-4 shrink-0 text-yellow-400" />
                    <span className="truncate text-sm">
                      {destinationQuery.data?.content?.name ??
                        t("page.drive.import.loading_folder")}
                    </span>
                  </div>
                </div>

                {isModSource && (
                  <label className="flex cursor-pointer items-center gap-2 text-sm">
                    <Checkbox
                      checked={createCollectionFolders}
                      onCheckedChange={(checked) => setCreateCollectionFolders(checked === true)}
                    />
                    {t("page.drive.import.create_collection_folder")}
                  </label>
                )}

                <div className="flex items-center justify-between text-xs">
                  <span className="text-muted-foreground">선택된 폴더</span>
                  <span className="font-medium tabular-nums">{selectedCount}개</span>
                </div>

                {isImporting && copyProgress && (
                  <div className="space-y-2 rounded-md border p-3">
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
                        copyProgress.total > 0
                          ? (copyProgress.current / copyProgress.total) * 100
                          : null
                      }
                    />
                    <p className="text-xs text-muted-foreground">
                      {t(`page.drive.import.progress.${copyProgress.phase}`)}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      전송 탭에서도 진행률을 확인할 수 있습니다.
                    </p>
                  </div>
                )}
              </div>

              <div className="border-t p-3">
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleBack}
                    disabled={isImporting}
                    className="flex-1"
                  >
                    <ArrowLeftIcon className="size-4" />
                    이전
                  </Button>
                  <Button onClick={handleImport} disabled={!canImport} className="flex-1">
                    {isImporting ? (
                      <Loader2Icon className="mr-2 size-4 animate-spin" />
                    ) : (
                      <CloudDownloadIcon className="mr-2 size-4" />
                    )}
                    가져오기
                  </Button>
                </div>
              </div>
            </div>

            <div className="flex flex-1 flex-col">
              <div className="flex items-center justify-between border-b px-4 py-2.5">
                <span className="text-sm font-medium">폴더 선택</span>
                <span className="text-xs text-muted-foreground">{selectedCount}개 선택됨</span>
              </div>
              <ScrollArea className="flex-1" viewportRef={setTreeViewport}>
                <div className="p-2">
                  {resolving ? (
                    <div className="flex items-center justify-center p-8 text-sm text-muted-foreground">
                      <Loader2Icon className="mr-2 size-4 animate-spin" />
                      폴더 목록을 불러오는 중...
                    </div>
                  ) : (
                    <ImportFolderTree
                      visibleNodes={visibleNodes}
                      expanded={expanded}
                      selected={selected}
                      selectedAncestorIds={selectedAncestorIds}
                      loadingIds={loadingIds}
                      scrollElement={treeViewport}
                      onToggle={handleToggle}
                      onExpand={handleExpand}
                      onCollapse={handleCollapse}
                    />
                  )}
                </div>
              </ScrollArea>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
