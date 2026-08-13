import {
  BisectStatusCards,
  isExcludeValidationMessage,
  statusColor,
} from "@renderer/components/tools/mod-bisect-views";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import type { ModInfo } from "@renderer/types/mod";
import type { BisectSnapshot } from "@shared/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface ModConflictFinderDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  game: string;
  mod: ModInfo | null;
}

export function ModConflictFinderDialog({
  open,
  onOpenChange,
  game,
  mod,
}: ModConflictFinderDialogProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [starting, setStarting] = useState(false);
  const startingRef = useRef(false);

  const { data: snapshot } = useQuery<BisectSnapshot | null>({
    queryKey: ["tools:bisectState"],
    queryFn: () => window.api.invoke("tools:bisectGetState"),
    enabled: open,
    refetchInterval: (query) => {
      const value = query.state.data;
      if (!value) return false;
      if (value.status === "scanning" || value.status === "reverting") return 500;
      return false;
    },
  });

  useEffect(() => {
    if (!open) return;
    const unsubscribe = window.api.on("tools:bisectState", (next) => {
      queryClient.setQueryData(["tools:bisectState"], next);
    });
    return unsubscribe;
  }, [queryClient, open]);

  const startMutation = useMutation({
    mutationFn: ({ game, modPath }: { game: string; modPath: string }) =>
      window.api.invoke("tools:bisectStart", game, [modPath]),
    onError: (err) => {
      if (isExcludeValidationMessage(err.message)) {
        toast.warning(t("page.tools.mod_bisect.exclude_invalid"));
      } else {
        toast.error(err.message);
      }
      void queryClient.invalidateQueries({ queryKey: ["tools:bisectState"] });
    },
  });

  const respondMutation = useMutation({
    mutationFn: (fixed: boolean) => window.api.invoke("tools:bisectRespond", fixed),
    onError: (err) => {
      toast.error(err.message);
      void queryClient.invalidateQueries({ queryKey: ["tools:bisectState"] });
    },
  });

  const undoMutation = useMutation({
    mutationFn: () => window.api.invoke("tools:bisectUndoLastRound"),
    onError: (err) => toast.error(err.message),
  });

  const cancelMutation = useMutation({
    mutationFn: () => window.api.invoke("tools:bisectCancel"),
    onError: (err) => toast.error(err.message),
  });

  const finalizeMutation = useMutation({
    mutationFn: (keepDisabled: string[]) => window.api.invoke("tools:bisectFinalize", keepDisabled),
    onError: (err) => toast.error(err.message),
  });

  const status = snapshot?.status ?? "idle";
  const isActive = status === "scanning" || status === "round";
  const sessionActive = isActive || status === "reverting";
  const hasUnfinalizedResult = status === "done" && !!snapshot?.finalBadPath;
  const canStart = !!game && !!mod?.isEnabled && !sessionActive && !hasUnfinalizedResult;
  const canCancel = !!snapshot && status !== "idle" && status !== "done" && status !== "cancelled";
  const isBusy =
    starting ||
    startMutation.isPending ||
    respondMutation.isPending ||
    undoMutation.isPending ||
    cancelMutation.isPending ||
    finalizeMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="w-[min(44rem,calc(100%-2rem))] max-w-none"
        onClick={(event) => event.stopPropagation()}
      >
        <DialogHeader>
          <DialogTitle>{t("page.mod.dialog.find-conflict.title")}</DialogTitle>
          <DialogDescription>
            {sessionActive || hasUnfinalizedResult
              ? t("page.mod.dialog.find-conflict.session-active")
              : t("page.mod.dialog.find-conflict.description", { name: mod?.name ?? "" })}
          </DialogDescription>
        </DialogHeader>

        {mod && !mod.isEnabled ? (
          <div className="rounded-md border border-amber-500/20 bg-amber-500/10 p-2 text-xs text-amber-600 dark:text-amber-400">
            {t("page.mod.dialog.find-conflict.mod-disabled-warning")}
          </div>
        ) : null}

        <div className="flex flex-wrap items-center gap-2">
          <Button
            onClick={() => {
              if (
                !game ||
                !mod?.isEnabled ||
                sessionActive ||
                hasUnfinalizedResult ||
                startingRef.current
              )
                return;
              startingRef.current = true;
              setStarting(true);
              void startMutation
                .mutateAsync({ game, modPath: mod.path })
                .catch(() => {
                  // startMutation.onError already reported the failure
                })
                .finally(() => {
                  startingRef.current = false;
                  setStarting(false);
                });
            }}
            disabled={!canStart || isBusy}
          >
            {t("page.tools.mod_bisect.start")}
          </Button>
          <Button
            variant="destructive"
            onClick={() => cancelMutation.mutate()}
            disabled={!canCancel || isBusy}
          >
            {t("page.tools.mod_bisect.cancel")}
          </Button>
          <span className={`font-mono text-xs ${statusColor(status)}`}>
            {t(`page.tools.mod_bisect.status.${status}`)}
          </span>
        </div>

        <BisectStatusCards
          snapshot={snapshot}
          busy={isBusy}
          onRespond={(fixed) => respondMutation.mutate(fixed)}
          onUndo={() => undoMutation.mutate()}
          onFinalize={(keepDisabled) => finalizeMutation.mutate(keepDisabled)}
        />
      </DialogContent>
    </Dialog>
  );
}
