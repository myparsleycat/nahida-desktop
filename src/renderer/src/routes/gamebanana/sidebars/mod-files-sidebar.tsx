import { Badge } from "@renderer/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { Logger } from "@renderer/lib/logger";
import type { TFunction } from "i18next";
import {
  FileArchiveIcon,
  HeartIcon,
  Loader2Icon,
  MessageSquareIcon,
  UsersIcon,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { ErrorState, StatCard } from "../shared/common";
import type { ModOverviewQuery } from "../types";
import { formatEpoch, formatNumber, getSubmissionFullPreviewUrl } from "../utils";

export function ModFilesSidebar({
  t,
  language,
  modOverviewQuery,
}: {
  t: TFunction;
  language: string;
  modOverviewQuery: ModOverviewQuery;
}) {
  const files = modOverviewQuery.data?.profile._aFiles ?? [];
  const previewUrl = modOverviewQuery.data
    ? getSubmissionFullPreviewUrl(modOverviewQuery.data.profile)
    : undefined;
  const [pendingFileId, setPendingFileId] = useState<number | null>(null);

  const handleDownload = async (fileId: number, fileUrl: string, title: string) => {
    if (pendingFileId !== null) return;

    setPendingFileId(fileId);
    try {
      await window.api.invoke("mod:downloadGameBananaFile", {
        fileUrl,
        title,
        previewUrl: previewUrl ?? null,
      });
    } catch (error) {
      toast.error("다운로드를 시작하지 못했습니다.");
      Logger.error(error, "ModFilesSidebar:handleDownload");
    } finally {
      setPendingFileId(null);
    }
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <Card>
        <CardContent>
          {modOverviewQuery.isLoading ? (
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-2">
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
            </div>
          ) : modOverviewQuery.error ? (
            <ErrorState title={t("page.gamebanana.error_title")} />
          ) : (
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-2">
              <StatCard
                icon={<HeartIcon className="size-4" />}
                label={t("page.gamebanana.stats.likes")}
                value={formatNumber(modOverviewQuery.data?.profile._nLikeCount, language)}
              />
              <StatCard
                icon={<MessageSquareIcon className="size-4" />}
                label={t("page.gamebanana.stats.posts")}
                value={formatNumber(modOverviewQuery.data?.profile._nPostCount, language)}
              />
              <StatCard
                icon={<UsersIcon className="size-4" />}
                label={t("page.gamebanana.stats.views")}
                value={formatNumber(modOverviewQuery.data?.profile._nViewCount, language)}
              />
              <StatCard
                icon={<FileArchiveIcon className="size-4" />}
                label={t("page.gamebanana.stats.downloads")}
                value={formatNumber(modOverviewQuery.data?.profile._nDownloadCount, language)}
              />
            </div>
          )}
        </CardContent>
      </Card>

      <Card className="flex min-h-0 flex-1 flex-col">
        <CardHeader>
          <div className="flex items-start justify-between gap-3">
            <div>
              <CardTitle>{t("page.gamebanana.files")}</CardTitle>
            </div>
            <Badge variant="outline">{formatNumber(files.length, language)}</Badge>
          </div>
        </CardHeader>
        <CardContent className="min-h-0 flex-1 overflow-hidden">
          <ScrollArea className="h-full min-h-0 pr-4">
            <div className="space-y-2">
              {modOverviewQuery.isLoading && (
                <>
                  <Skeleton className="h-16 w-full" />
                  <Skeleton className="h-16 w-full" />
                  <Skeleton className="h-16 w-full" />
                </>
              )}
              {modOverviewQuery.error && <ErrorState title={t("page.gamebanana.error_title")} />}
              {!modOverviewQuery.isLoading && !modOverviewQuery.error && files.length === 0 && (
                <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
                  {t("page.gamebanana.no_files")}
                </div>
              )}
              {!modOverviewQuery.isLoading &&
                !modOverviewQuery.error &&
                files.map((file) => {
                  const isPending = pendingFileId === file._idRow;

                  return (
                    <button
                      key={file._idRow}
                      type="button"
                      disabled={pendingFileId !== null}
                      aria-busy={isPending}
                      className="w-full overflow-hidden rounded-xl border px-3 py-3 text-left transition-colors hover:bg-muted/40 disabled:cursor-wait disabled:opacity-70"
                      onClick={() =>
                        void handleDownload(file._idRow, file._sDownloadUrl, file._sFile)
                      }
                    >
                      <div className="flex items-start gap-2">
                        {isPending && (
                          <Loader2Icon className="mt-0.5 size-4 shrink-0 animate-spin text-muted-foreground" />
                        )}
                        <div className="min-w-0 flex-1">
                          <div className="line-clamp-2 break-all text-sm font-medium">
                            {file._sFile}
                          </div>
                          <div className="mt-2 min-w-0 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                            <span>
                              {t("page.gamebanana.file_downloads", {
                                count: formatNumber(file._nDownloadCount, language),
                              })}
                            </span>
                            <span>{formatEpoch(file._tsDateAdded, language)}</span>
                            {isPending && <span>준비중...</span>}
                          </div>
                        </div>
                      </div>
                    </button>
                  );
                })}
            </div>
          </ScrollArea>
        </CardContent>
      </Card>
    </div>
  );
}
