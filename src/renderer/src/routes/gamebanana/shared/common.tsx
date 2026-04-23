import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { Loader2Icon, RefreshCwIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

export function PaginationButtons({
  page,
  onPrev,
  onNext,
  disablePrev,
  disableNext,
}: {
  page: number;
  onPrev: () => void;
  onNext: () => void;
  disablePrev: boolean;
  disableNext: boolean;
}) {
  const { t } = useTranslation();

  return (
    <div className="flex items-center gap-2">
      <Button variant="outline" size="sm" disabled={disablePrev} onClick={onPrev}>
        {t("page.gamebanana.previous")}
      </Button>
      <Badge variant="outline">#{page}</Badge>
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

export function ErrorState({ title }: { title: string }) {
  const { t } = useTranslation();

  return (
    <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
      <div className="mb-2 font-medium text-foreground">{title}</div>
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
}: {
  title: string;
  description: string;
  actionLabel?: string;
  pending?: boolean;
  onAction?: () => void;
}) {
  return (
    <div className="flex h-full min-h-80 items-center justify-center">
      <div className="w-full max-w-md rounded-xl border border-dashed p-6 text-center">
        <div className="text-base font-medium">{title}</div>
        <div className="mt-2 text-sm text-muted-foreground">{description}</div>
        {actionLabel && onAction && (
          <Button className="mt-4" onClick={onAction} disabled={pending}>
            {pending ? (
              <Loader2Icon className="size-4 animate-spin" />
            ) : (
              <RefreshCwIcon className="size-4" />
            )}
            {actionLabel}
          </Button>
        )}
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
