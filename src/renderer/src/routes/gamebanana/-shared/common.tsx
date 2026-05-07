import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { Loader2Icon, RefreshCwIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

export function PaginationButtons({
  page,
  totalPages,
  onPrev,
  onNext,
  onPageChange,
  disablePrev,
  disableNext,
}: {
  page: number;
  totalPages?: number;
  onPrev: () => void;
  onNext: () => void;
  onPageChange: (page: number) => void;
  disablePrev: boolean;
  disableNext: boolean;
}) {
  const { t } = useTranslation();
  const [inputValue, setInputValue] = useState(String(page));
  const [isEditing, setIsEditing] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const commitPage = useCallback(() => {
    setIsEditing(false);
    const parsed = parseInt(inputValue, 10);
    if (!Number.isNaN(parsed) && parsed >= 1) {
      const maxPage = totalPages ?? Infinity;
      const clamped = Math.min(parsed, maxPage);
      if (clamped !== page) {
        onPageChange(clamped);
      } else {
        setInputValue(String(page));
      }
    } else {
      setInputValue(String(page));
    }
  }, [inputValue, page, totalPages, onPageChange]);

  return (
    <div className="flex items-center gap-2">
      <Button variant="outline" size="sm" disabled={disablePrev} onClick={onPrev}>
        {t("page.gamebanana.previous")}
      </Button>
      <div className="flex items-center gap-1">
        <Input
          ref={inputRef}
          className="h-7 w-14 text-center text-sm"
          hideFocusRing
          value={isEditing ? inputValue : String(page)}
          onChange={(e) => {
            setInputValue(e.target.value);
            if (!isEditing) setIsEditing(true);
          }}
          onFocus={() => {
            setIsEditing(true);
            setInputValue(String(page));
            inputRef.current?.select();
          }}
          onBlur={commitPage}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              commitPage();
              inputRef.current?.blur();
            } else if (e.key === "Escape") {
              setInputValue(String(page));
              setIsEditing(false);
              inputRef.current?.blur();
            }
          }}
        />
        {totalPages != null && (
          <span className="text-xs text-muted-foreground">/ {totalPages}</span>
        )}
      </div>
      <Button variant="outline" size="sm" disabled={disableNext} onClick={onNext}>
        {t("page.gamebanana.next")}
      </Button>
    </div>
  );
}

export function StatCard({
  icon,
  label,
  value,
}: {
  icon: ReactNode;
  label: string;
  value: string;
}) {
  return (
    <div className="rounded-xl border bg-muted/20 p-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        {icon}
        <span>{label}</span>
      </div>
      <div className="mt-2 text-lg font-semibold">{value}</div>
    </div>
  );
}

export function ErrorState({
  title,
  description,
  details,
}: {
  title: string;
  description?: string;
  details?: string;
}) {
  const { t } = useTranslation();

  return (
    <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
      <div className="mb-2 font-medium text-foreground">{title}</div>
      {description ? <div className="mb-3">{description}</div> : null}
      {details ? (
        <div className="mb-3 break-words rounded-md bg-muted/50 px-3 py-2 text-left text-xs">
          {details}
        </div>
      ) : null}
      <div className="flex items-center justify-center gap-2">
        <RefreshCwIcon className="size-4" />
        <span>{t("page.gamebanana.retry_later")}</span>
      </div>
    </div>
  );
}

export function GameBananaAuthState({
  title,
  description,
  actionLabel,
  pending = false,
  onAction,
  extraAction,
}: {
  title: string;
  description: string;
  actionLabel?: string;
  pending?: boolean;
  onAction?: () => void;
  extraAction?: ReactNode;
}) {
  return (
    <div className="flex h-full min-h-80 items-center justify-center">
      <div className="w-full max-w-md rounded-xl border border-dashed p-6 text-center">
        <div className="text-base font-medium">{title}</div>
        <div className="mt-2 text-sm text-muted-foreground">{description}</div>
        {(actionLabel && onAction) || extraAction ? (
          <div className="mt-4 flex items-center justify-center gap-2">
            {actionLabel && onAction && (
              <Button onClick={onAction} disabled={pending}>
                {pending ? (
                  <Loader2Icon className="size-4 animate-spin" />
                ) : (
                  <RefreshCwIcon className="size-4" />
                )}
                {actionLabel}
              </Button>
            )}
            {extraAction}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function OverviewSkeleton() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-24 w-full" />
      <Skeleton className="h-24 w-full" />
      <Skeleton className="h-24 w-full" />
    </div>
  );
}
