import { Avatar, AvatarFallback, AvatarImage } from "@renderer/components/ui/avatar";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader } from "@renderer/components/ui/card";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Skeleton } from "@renderer/components/ui/skeleton";
import {
  useGameBananaModPosts,
  type GameBananaModPostsSort,
  type GameBananaSubmissionSelection,
} from "@renderer/hooks/use-gamebanana-data";
import { cn } from "@renderer/lib/utils";
import DOMPurify from "dompurify";
import type { TFunction } from "i18next";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  ImageIcon,
  Loader2Icon,
  MessageSquareIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { ErrorState } from "../-shared/common";
import { getGameBananaErrorPresentation } from "../-shared/errors";
import type { ModOverviewQuery } from "../-types";
import { formatEpoch, formatNumber, getSubmissionPreviewImages } from "../-utils";

function SubmissionPreviewLightbox({
  previews,
  previewIndex,
  preview,
  open,
  onOpenChange,
  onPreviewIndexChange,
}: {
  previews: { fullUrl: string; alt: string }[];
  previewIndex: number | null;
  preview: { fullUrl: string; alt: string } | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPreviewIndexChange: (index: number) => void;
}) {
  const canNavigate = previews.length > 1 && previewIndex !== null;

  useEffect(() => {
    if (!open) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onOpenChange(false);
      }
      if (event.key === "ArrowLeft" && canNavigate && previewIndex !== null) {
        event.preventDefault();
        onPreviewIndexChange((previewIndex - 1 + previews.length) % previews.length);
      }
      if (event.key === "ArrowRight" && canNavigate && previewIndex !== null) {
        event.preventDefault();
        onPreviewIndexChange((previewIndex + 1) % previews.length);
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [canNavigate, onOpenChange, onPreviewIndexChange, open, previewIndex, previews.length]);

  if (!open || !preview) {
    return null;
  }

  return createPortal(
    <div
      className="fixed inset-0 z-100 flex items-center justify-center bg-black/80 backdrop-blur-sm"
      onClick={() => onOpenChange(false)}
      onContextMenu={(event) => event.stopPropagation()}
    >
      {canNavigate && previewIndex !== null && (
        <Button
          variant="ghost"
          size="icon"
          className="absolute left-4 text-white hover:bg-white/20 hover:text-white"
          onClick={(event) => {
            event.stopPropagation();
            onPreviewIndexChange((previewIndex - 1 + previews.length) % previews.length);
          }}
        >
          <ChevronLeftIcon className="size-6" />
        </Button>
      )}

      {canNavigate && previewIndex !== null && (
        <Button
          variant="ghost"
          size="icon"
          className="absolute right-16 text-white hover:bg-white/20 hover:text-white"
          onClick={(event) => {
            event.stopPropagation();
            onPreviewIndexChange((previewIndex + 1) % previews.length);
          }}
        >
          <ChevronRightIcon className="size-6" />
        </Button>
      )}

      <Button
        variant="ghost"
        size="icon"
        className="absolute top-10 right-4 text-white hover:bg-white/20 hover:text-white"
        onClick={(event) => {
          event.stopPropagation();
          onOpenChange(false);
        }}
      >
        <XIcon className="size-6" />
      </Button>

      <div
        className="relative flex h-[88vh] w-[88vw] items-center justify-center"
        onClick={(event) => event.stopPropagation()}
      >
        <img
          src={preview.fullUrl}
          alt={preview.alt}
          className="max-h-full max-w-full rounded-md object-contain shadow-2xl"
          draggable={false}
        />
      </div>

      {canNavigate && previewIndex !== null && (
        <div className="absolute bottom-6 rounded-full bg-black/50 px-3 py-1 text-xs text-white">
          {previewIndex + 1} / {previews.length}
        </div>
      )}
    </div>,
    document.body,
  );
}

function getPosterInitials(name?: string) {
  if (!name) return "?";

  return (
    name
      .trim()
      .split(/\s+/)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase() ?? "")
      .join("") || "?"
  );
}

function stripHtmlToText(value: string) {
  if (!value) return "";

  if (typeof DOMParser !== "undefined") {
    const documentNode = new DOMParser().parseFromString(value, "text/html");
    const text = documentNode.body.textContent ?? "";
    return text
      .replace(/\n{3,}/g, "\n\n")
      .replace(/[ \t]{2,}/g, " ")
      .trim();
  }

  return value
    .replace(/<br\s*\/?>/gi, "\n")
    .replace(/<\/p>/gi, "\n\n")
    .replace(/<[^>]+>/g, " ")
    .replace(/\n{3,}/g, "\n\n")
    .replace(/[ \t]{2,}/g, " ")
    .trim();
}

function sanitizeGameBananaHtml(value: string) {
  if (!value) return "";

  return DOMPurify.sanitize(value, {
    FORBID_TAGS: ["script", "style", "iframe", "object", "embed"],
    ALLOWED_URI_REGEXP: /^(https?:|mailto:|#)/i,
  });
}

function hardenSanitizedGameBananaAnchors(value: string) {
  if (!value || typeof document === "undefined") {
    return value;
  }

  const template = document.createElement("template");
  template.innerHTML = value;

  template.content.querySelectorAll("a").forEach((anchor) => {
    anchor.setAttribute("target", "_blank");
    anchor.setAttribute("rel", "noreferrer noopener");
  });

  return template.innerHTML;
}

function CommentSkeletonList() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-24 w-full rounded-xl" />
      <Skeleton className="h-24 w-full rounded-xl" />
      <Skeleton className="h-24 w-full rounded-xl" />
    </div>
  );
}

export function ModDetailPanel({
  t,
  language,
  selection,
  modOverviewQuery,
}: {
  t: TFunction;
  language: string;
  selection?: GameBananaSubmissionSelection;
  modOverviewQuery: ModOverviewQuery;
}) {
  const modId = selection?.id;
  const modelName = selection?.modelName ?? "Mod";
  const previews = modOverviewQuery.data
    ? getSubmissionPreviewImages(modOverviewQuery.data.profile)
    : [];
  const maxVisiblePreviews = 8;
  const hasOverflowPreviews = previews.length > maxVisiblePreviews;
  const visiblePreviews = hasOverflowPreviews
    ? previews.slice(0, maxVisiblePreviews - 1)
    : previews;
  const hiddenPreviewCount = previews.length - (maxVisiblePreviews - 1);
  const [lightboxPreviewIndex, setLightboxPreviewIndex] = useState<number | null>(null);
  const [commentSort, setCommentSort] = useState<GameBananaModPostsSort>("popular");
  const commentsQuery = useGameBananaModPosts(modId, modelName, 1, commentSort, Boolean(modId));
  const modErrorPresentation = getGameBananaErrorPresentation(modOverviewQuery.error, t);
  const commentsErrorPresentation = getGameBananaErrorPresentation(commentsQuery.error, t);
  const lightboxPreview =
    lightboxPreviewIndex === null ? null : (previews[lightboxPreviewIndex] ?? null);

  useEffect(() => {
    setLightboxPreviewIndex(null);
  }, [modOverviewQuery.data?.profile._idRow]);

  useEffect(() => {
    setCommentSort("popular");
  }, [modId, modelName]);

  const comments = useMemo(() => {
    const seen = new Set<number>();

    return (commentsQuery.data?.pages ?? [])
      .flatMap((page) => page._aRecords)
      .filter((comment) => comment._nStatus !== "3")
      .filter((comment) => {
        if (seen.has(comment._idRow)) return false;
        seen.add(comment._idRow);
        return true;
      });
  }, [commentsQuery.data]);
  const descriptionHtml = useMemo(
    () =>
      hardenSanitizedGameBananaAnchors(
        sanitizeGameBananaHtml(modOverviewQuery.data?.profile._sText ?? ""),
      ),
    [modOverviewQuery.data?.profile._sText],
  );

  const totalCommentCount =
    commentsQuery.data?.pages[0]?._aMetadata._nRecordCount ?? comments.length;
  const lastCommentsPage = commentsQuery.data?.pages[commentsQuery.data.pages.length - 1];
  const hasMoreComments = Boolean(lastCommentsPage && !lastCommentsPage._aMetadata._bIsComplete);
  const isCommentsInitialLoading = !commentsQuery.data && commentsQuery.isFetching;

  return (
    <>
      <ScrollArea
        className="h-full min-h-0 min-w-0"
        viewportClassName="overflow-x-hidden [&>div]:!block [&>div]:!min-w-0 [&>div]:!w-full [&>div]:max-w-full"
      >
        <div className="min-w-0 max-w-full space-y-4 p-4">
          <Card>
            <CardContent>
              {modOverviewQuery.isLoading ? (
                <div className="space-y-3">
                  <div className="flex gap-2">
                    <Skeleton className="h-6 w-20 rounded-full" />
                    <Skeleton className="h-6 w-24 rounded-full" />
                  </div>
                  <Skeleton className="h-9 w-2/3" />
                </div>
              ) : modOverviewQuery.error ? (
                <ErrorState
                  title={t("page.gamebanana.error_title")}
                  description={modErrorPresentation.description}
                  details={modErrorPresentation.details}
                />
              ) : modOverviewQuery.data ? (
                <div className="space-y-2">
                  <div className="flex flex-wrap gap-2">
                    <Badge variant="outline">
                      {modOverviewQuery.data.profile._aCategory._sName}
                    </Badge>
                    <Badge variant="outline">
                      {modOverviewQuery.data.profile._aSubmitter._sName}
                    </Badge>
                    <Badge variant="outline">{modOverviewQuery.data.profile._aGame._sName}</Badge>
                  </div>
                  <div className="text-2xl font-semibold">
                    {modOverviewQuery.data.profile._sName}
                  </div>
                </div>
              ) : null}
            </CardContent>
          </Card>

          <Card>
            <CardContent>
              {modOverviewQuery.isLoading ? (
                <div className="grid grid-cols-2 auto-rows-[140px] gap-2 sm:grid-cols-5 sm:auto-rows-[110px]">
                  <Skeleton className="col-span-2 row-span-2 h-full rounded-2xl sm:col-span-3 sm:row-span-4" />
                  <Skeleton className="h-full rounded-2xl sm:col-span-2 sm:row-span-2" />
                  <Skeleton className="h-full rounded-2xl sm:col-span-2 sm:row-span-2" />
                  <Skeleton className="h-full rounded-2xl" />
                  <Skeleton className="h-full rounded-2xl" />
                </div>
              ) : modOverviewQuery.error ? (
                <ErrorState
                  title={t("page.gamebanana.error_title")}
                  description={modErrorPresentation.description}
                  details={modErrorPresentation.details}
                />
              ) : previews.length > 0 ? (
                <div className="grid grid-cols-2 auto-rows-[140px] gap-2 sm:grid-cols-5 sm:auto-rows-[110px]">
                  {visiblePreviews.map((preview, index) => (
                    <button
                      key={preview.id}
                      type="button"
                      className={cn(
                        "group relative h-full overflow-hidden rounded-2xl border bg-muted/20 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                        index === 0 && "col-span-2 row-span-2 sm:col-span-3 sm:row-span-4",
                        index === 1 && "sm:col-span-2 sm:row-span-2",
                        index === 2 && "sm:col-span-2 sm:row-span-2 sm:col-start-4 sm:row-start-3",
                        index === 3 && "sm:col-start-1 sm:row-start-5",
                        index === 4 && "sm:col-start-2 sm:row-start-5",
                        index === 5 && "sm:col-start-3 sm:row-start-5",
                        index === 6 && "sm:col-start-4 sm:row-start-5",
                      )}
                      onClick={() => setLightboxPreviewIndex(index)}
                    >
                      <div className="absolute inset-0 bg-linear-to-t from-black/20 via-transparent to-transparent opacity-0 transition-opacity group-hover:opacity-100" />
                      <img
                        src={preview.previewUrl}
                        alt={preview.alt}
                        className="h-full w-full object-cover"
                        loading="lazy"
                        style={{
                          imageRendering: "-webkit-optimize-contrast",
                          transform: "translateZ(0)",
                          backfaceVisibility: "hidden",
                        }}
                      />
                    </button>
                  ))}
                  {hasOverflowPreviews && (
                    <button
                      type="button"
                      className="group relative h-full overflow-hidden rounded-2xl border bg-muted/20 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:col-start-5 sm:row-start-5"
                      onClick={() => setLightboxPreviewIndex(maxVisiblePreviews - 1)}
                    >
                      <img
                        src={previews[maxVisiblePreviews - 1]?.previewUrl}
                        alt={previews[maxVisiblePreviews - 1]?.alt ?? "More preview images"}
                        className="h-full w-full object-cover"
                        loading="lazy"
                      />
                      <div className="absolute inset-0 flex items-center justify-center bg-black/60 text-center text-sm font-semibold text-white">
                        +{hiddenPreviewCount}
                      </div>
                    </button>
                  )}
                </div>
              ) : (
                <div className="flex aspect-16/10 w-full items-center justify-center rounded-2xl border bg-muted/20 text-muted-foreground">
                  <div className="flex flex-col items-center gap-2 text-sm">
                    <ImageIcon className="size-8" />
                    <span>{t("page.gamebanana.no_preview_image")}</span>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardContent>
              {modOverviewQuery.isLoading ? (
                <div className="space-y-3">
                  <Skeleton className="h-5 w-2/5" />
                  <Skeleton className="h-5 w-full" />
                  <Skeleton className="h-5 w-11/12" />
                  <Skeleton className="h-5 w-4/5" />
                  <Skeleton className="h-24 w-full rounded-xl" />
                </div>
              ) : modOverviewQuery.error ? (
                <ErrorState
                  title={t("page.gamebanana.error_title")}
                  description={modErrorPresentation.description}
                  details={modErrorPresentation.details}
                />
              ) : modOverviewQuery.data ? (
                descriptionHtml.trim() ? (
                  <div
                    className="[&_a]:text-primary [&_a]:underline-offset-4 hover:[&_a]:underline [&_br]:leading-6 [&_h1]:text-2xl [&_h1]:font-semibold [&_h1]:leading-tight [&_h2]:text-xl [&_h2]:font-semibold [&_h2]:leading-tight [&_h3]:text-lg [&_h3]:font-semibold [&_img]:max-w-full [&_img]:rounded-xl [&_li]:ml-5 [&_li]:list-disc [&_ol]:space-y-2 [&_p]:leading-7 [&_p]:not-last:mb-4 [&_span]:wrap-break-word [&_strong]:font-semibold [&_ul]:space-y-2"
                    dangerouslySetInnerHTML={{ __html: descriptionHtml }}
                  />
                ) : (
                  <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
                    {t("page.gamebanana.no_description")}
                  </div>
                )
              ) : null}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="flex flex-wrap items-center justify-end gap-2">
                  <Badge variant="outline">{formatNumber(totalCommentCount, language)}</Badge>
                  <Select
                    value={commentSort}
                    onValueChange={(value) => setCommentSort(value as GameBananaModPostsSort)}
                  >
                    <SelectTrigger className="h-7">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      <SelectGroup>
                        {(["popular", "newest"] as const).map((sort) => (
                          <SelectItem key={sort} value={sort}>
                            {t(`page.gamebanana.comment_sort.${sort}`)}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </CardHeader>
            <CardContent className="space-y-3">
              {isCommentsInitialLoading && <CommentSkeletonList />}
              {commentsQuery.error && (
                <ErrorState
                  title={t("page.gamebanana.error_title")}
                  description={commentsErrorPresentation.description}
                  details={commentsErrorPresentation.details}
                />
              )}
              {!isCommentsInitialLoading && !commentsQuery.error && comments.length === 0 && (
                <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
                  {t("page.gamebanana.no_comments")}
                </div>
              )}
              {!commentsQuery.error &&
                comments.map((comment) => {
                  const posterName = comment._aPoster?._sName ?? t("g.unknown");
                  const body = stripHtmlToText(comment._sText);
                  const stampCount =
                    comment._aStamps?.reduce((sum, stamp) => sum + (stamp._nCount ?? 0), 0) ?? 0;

                  return (
                    <div key={comment._idRow} className="rounded-xl border bg-muted/10 p-3">
                      <div className="flex items-start gap-3">
                        <Avatar size="sm" className="mt-0.5">
                          <AvatarImage src={comment._aPoster?._sAvatarUrl} alt={posterName} />
                          <AvatarFallback>{getPosterInitials(posterName)}</AvatarFallback>
                        </Avatar>
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                            <span className="font-medium">{posterName}</span>
                            {comment._tsDateAdded && (
                              <span className="text-xs text-muted-foreground">
                                {formatEpoch(comment._tsDateAdded, language)}
                              </span>
                            )}
                          </div>
                          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                            <span className="inline-flex items-center gap-1">
                              <MessageSquareIcon className="size-3.5" />
                              {t("page.gamebanana.comment_replies", {
                                count: formatNumber(comment._nReplyCount ?? 0, language),
                              })}
                            </span>
                            <span>
                              {t("page.gamebanana.comment_stamps", {
                                count: formatNumber(stampCount, language),
                              })}
                            </span>
                          </div>
                          <p className="mt-3 whitespace-pre-wrap wrap-break-word text-sm leading-6">
                            {body || comment._sText}
                          </p>
                        </div>
                      </div>
                    </div>
                  );
                })}
              {hasMoreComments && !commentsQuery.error && (
                <div className="flex justify-center pt-1">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={commentsQuery.isFetchingNextPage}
                    onClick={() => void commentsQuery.fetchNextPage()}
                  >
                    {commentsQuery.isFetchingNextPage && (
                      <Loader2Icon className="size-4 animate-spin" />
                    )}
                    {t("page.gamebanana.load_more_comments")}
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </ScrollArea>

      <SubmissionPreviewLightbox
        previews={previews}
        previewIndex={lightboxPreviewIndex}
        preview={lightboxPreview}
        open={lightboxPreview !== null}
        onOpenChange={(open) => {
          if (!open) {
            setLightboxPreviewIndex(null);
          }
        }}
        onPreviewIndexChange={(index) => setLightboxPreviewIndex(index)}
      />
    </>
  );
}
