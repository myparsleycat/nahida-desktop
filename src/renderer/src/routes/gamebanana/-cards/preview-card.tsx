import { cn } from "@renderer/lib/utils";
import { ExternalLinkIcon, ImageIcon } from "lucide-react";
import type { SubmissionListItem } from "../-types";
import {
  formatEpoch,
  formatNumber,
  getSubmissionPreviewUrl,
  getSubmissionTimestamp,
} from "../-utils";

export function PreviewCard({
  item,
  language,
  onClick,
  active = false,
  hoverClassName = "hover:bg-muted/40",
}: {
  item: SubmissionListItem;
  language: string;
  onClick: () => void;
  active?: boolean;
  hoverClassName?: string;
}) {
  const displayTimestamp = getSubmissionTimestamp(item);
  const previewUrl = getSubmissionPreviewUrl(item);

  return (
    <button
      type="button"
      className={cn(
        "w-full overflow-hidden rounded-2xl border bg-card text-left transition-colors",
        active ? "border-primary bg-primary/8" : hoverClassName,
      )}
      onClick={onClick}
    >
      <div className="aspect-16/10 overflow-hidden bg-muted/40">
        {previewUrl ? (
          <img
            src={previewUrl}
            alt={item._sName}
            className="h-full w-full object-cover transition-transform duration-200 hover:scale-[1.02]"
            loading="lazy"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-muted-foreground">
            <div className="flex flex-col items-center gap-2 text-xs">
              <ImageIcon className="size-8" />
              <span>No preview</span>
            </div>
          </div>
        )}
      </div>

      <div className="space-y-3 p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 space-y-1">
            <div className="line-clamp-1 font-medium">{item._sName}</div>
            <div className="line-clamp-2 text-xs text-muted-foreground">
              {item._sDescription || item._aSubCategory?._sName || item._aRootCategory?._sName}
            </div>
          </div>
          <ExternalLinkIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        </div>

        <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
          <span>{item._aSubmitter._sName}</span>
          {typeof displayTimestamp === "number" && (
            <span>{formatEpoch(displayTimestamp, language)}</span>
          )}
        </div>

        <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
          {typeof item._nLikeCount === "number" && (
            <span>❤ {formatNumber(item._nLikeCount, language)}</span>
          )}
          {typeof item._nPostCount === "number" && (
            <span>💬 {formatNumber(item._nPostCount, language)}</span>
          )}
          {typeof item._nViewCount === "number" && (
            <span>👁 {formatNumber(item._nViewCount, language)}</span>
          )}
        </div>
      </div>
    </button>
  );
}
