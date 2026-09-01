import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@renderer/components/ui/tabs";
import type { useModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import {
  AlertTriangleIcon,
  FileClockIcon,
  Loader2Icon,
  RefreshCwIcon,
  RotateCcwIcon,
  Trash2Icon,
  WrenchIcon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import zzmiModFixerIcon from "@/renderer/assets/img/zzmi-mod-fixer-icon.png";

type ModFixRunner = ReturnType<typeof useModFixRunner>;

export function ZZMIModFixerDialog({ runner }: { runner: ModFixRunner }) {
  const { t } = useTranslation();
  const key = "page.mod.dialog.zzmi-fix-runner";
  const [pendingDelete, setPendingDelete] = useState<{
    sessionId: string;
    entryId?: string;
  } | null>(null);
  const rules = runner.zzmiPrepare?.rules;

  return (
    <>
      <Dialog open={runner.showZZMIDialog} onOpenChange={runner.setShowZZMIDialog}>
        <DialogContent className="flex max-h-[88vh] max-w-4xl min-w-lg flex-col overflow-hidden">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <img src={zzmiModFixerIcon} className="size-7 rounded" />
              ZZMI Mod Fixer
            </DialogTitle>
            <DialogDescription className="break-all">{runner.activeModPath}</DialogDescription>
          </DialogHeader>

          <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-xs">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="secondary">{rules?.activeTag ?? t("g.unknown")}</Badge>
              <span className="font-mono text-muted-foreground">
                {rules?.activeCommit?.slice(0, 8) ?? "--------"}
              </span>
              <span className="text-muted-foreground">
                {t(`${key}.rules.source`, { source: rules?.activeSource ?? "embedded" })}
              </span>
            </div>
            <Button
              size="sm"
              variant="outline"
              disabled={runner.zzmiUpdateBusy}
              onClick={() => void runner.handleRefreshZZMIRules()}
            >
              {runner.zzmiUpdateBusy ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />}
              {t(`${key}.rules.refresh`)}
            </Button>
          </div>

          {rules?.updateAvailable ? (
            <div className="flex items-center justify-between gap-3 rounded-lg border border-sky-400/40 bg-sky-500/10 p-3 text-sm">
              <div>
                <p className="font-semibold">{t(`${key}.update.available`)}</p>
                <p className="text-xs text-muted-foreground">
                  {rules.activeTag} → {rules.latestTag ?? t("g.unknown")}
                </p>
              </div>
              <Button
                size="sm"
                disabled={runner.zzmiUpdateBusy}
                onClick={() => void runner.handleUpdateZZMIRules()}
              >
                {runner.zzmiUpdateBusy && <Loader2Icon className="animate-spin" />}
                {t(`${key}.update.action`)}
              </Button>
            </div>
          ) : null}

          {rules?.incompatibilityReason ? (
            <div className="flex gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-300">
              <AlertTriangleIcon className="size-4 shrink-0" />
              <span>{rules.incompatibilityReason}</span>
            </div>
          ) : null}

          <Tabs
            value={runner.zzmiTab}
            onValueChange={(value) =>
              runner.setZZMITab(value as "hash" | "jane" | "dialyn" | "rollback")
            }
            className="min-h-0 flex-1"
          >
            <TabsList className="grid w-full grid-cols-4">
              <TabsTrigger value="hash">{t(`${key}.tabs.hash`)}</TabsTrigger>
              <TabsTrigger value="jane">{t(`${key}.tabs.jane`)}</TabsTrigger>
              <TabsTrigger value="dialyn">{t(`${key}.tabs.dialyn`)}</TabsTrigger>
              <TabsTrigger value="rollback">{t(`${key}.tabs.rollback`)}</TabsTrigger>
            </TabsList>

            {(["hash", "jane", "dialyn"] as const).map((tool) => (
              <TabsContent key={tool} value={tool} className="mt-4">
                <div className="space-y-4 rounded-xl border bg-card p-5">
                  <div className="flex items-start gap-3">
                    <div className="rounded-lg bg-primary/10 p-2 text-primary">
                      <WrenchIcon className="size-5" />
                    </div>
                    <div className="space-y-1">
                      <h3 className="font-semibold">{t(`${key}.${tool}.title`)}</h3>
                      <p className="text-sm leading-relaxed text-muted-foreground">
                        {t(`${key}.${tool}.description`)}
                      </p>
                    </div>
                  </div>
                  <div className="flex justify-end">
                    <Button onClick={() => void runner.handleRunZZMIFixer(tool)}>
                      {t(`${key}.run.action`)}
                    </Button>
                  </div>
                </div>
              </TabsContent>
            ))}

            <TabsContent value="rollback" className="mt-4 min-h-0">
              <div className="mb-3 flex items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">
                  {t(`${key}.rollback.summary`, {
                    count: runner.zzmiBackups.length,
                    size: formatBytes(
                      runner.zzmiBackups.reduce((total, session) => total + session.size, 0),
                    ),
                  })}
                </span>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={runner.zzmiRollbackBusy}
                    onClick={() => void runner.refreshZZMIBackups()}
                  >
                    <RefreshCwIcon />
                    {t(`${key}.rules.refresh`)}
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    disabled={runner.zzmiBackups.length === 0 || runner.zzmiRollbackBusy}
                    onClick={() => runner.setShowZZMICleanConfirm(true)}
                  >
                    <Trash2Icon />
                    {t(`${key}.rollback.delete_all`)}
                  </Button>
                </div>
              </div>

              <ScrollArea className="h-[380px] rounded-lg border">
                <div className="space-y-3 p-3">
                  {runner.isLoadingZZMIBackups ? (
                    <div className="flex justify-center p-8">
                      <Loader2Icon className="animate-spin" />
                    </div>
                  ) : runner.zzmiBackups.length === 0 ? (
                    <p className="p-8 text-center text-sm text-muted-foreground">
                      {t(`${key}.rollback.empty`)}
                    </p>
                  ) : (
                    runner.zzmiBackups.map((session) => (
                      <div key={session.id} className="space-y-3 rounded-lg border bg-card p-3">
                        <div className="flex flex-wrap items-start justify-between gap-2">
                          <div className="flex gap-2">
                            <FileClockIcon className="mt-0.5 size-4 text-primary" />
                            <div>
                              <div className="flex flex-wrap items-center gap-2 text-sm font-semibold">
                                {t(`${key}.tool.${session.tool}`)}
                                <Badge
                                  variant={
                                    session.status === "completed" ? "secondary" : "destructive"
                                  }
                                >
                                  {session.status}
                                </Badge>
                              </div>
                              <p className="text-[11px] text-muted-foreground">
                                {new Date(session.createdAt).toLocaleString()} · {session.ruleTag} ·{" "}
                                {formatBytes(session.size)}
                              </p>
                            </div>
                          </div>
                          <div className="flex gap-2">
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={runner.zzmiRollbackBusy}
                              onClick={() => void runner.restoreZZMI(session.id)}
                            >
                              <RotateCcwIcon />
                              {t(`${key}.rollback.restore_session`)}
                            </Button>
                            <Button
                              size="icon-sm"
                              variant="ghost"
                              disabled={runner.zzmiRollbackBusy}
                              onClick={() => setPendingDelete({ sessionId: session.id })}
                            >
                              <Trash2Icon />
                            </Button>
                          </div>
                        </div>
                        <div className="space-y-1 rounded-md bg-muted/40 p-2">
                          {(session.entries ?? []).map((entry) => (
                            <div
                              key={entry.id}
                              className="flex items-center justify-between gap-2 text-xs"
                            >
                              <span
                                className="min-w-0 flex-1 truncate font-mono"
                                title={entry.relativePath}
                              >
                                {entry.relativePath}
                              </span>
                              <Badge variant="outline">{entry.kind}</Badge>
                              <Button
                                size="icon-sm"
                                variant="ghost"
                                disabled={runner.zzmiRollbackBusy}
                                onClick={() => void runner.restoreZZMI(session.id, entry.id)}
                              >
                                <RotateCcwIcon />
                              </Button>
                              <Button
                                size="icon-sm"
                                variant="ghost"
                                disabled={runner.zzmiRollbackBusy}
                                onClick={() =>
                                  setPendingDelete({ sessionId: session.id, entryId: entry.id })
                                }
                              >
                                <Trash2Icon />
                              </Button>
                            </div>
                          ))}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </ScrollArea>
            </TabsContent>
          </Tabs>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={runner.zzmiPendingRestore !== null}
        onOpenChange={(open) => {
          if (!open) {
            runner.setZZMIPendingRestore(null);
            runner.setZZMIConflicts([]);
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(`${key}.rollback.conflict_title`)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(`${key}.rollback.conflict_description`, { count: runner.zzmiConflicts.length })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <ScrollArea className="max-h-48 rounded border p-2 text-xs">
            {runner.zzmiConflicts.map((conflict) => (
              <div key={conflict.entryId} className="font-mono break-all">
                {conflict.originalPath}
              </div>
            ))}
          </ScrollArea>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                const pending = runner.zzmiPendingRestore;
                if (pending) void runner.restoreZZMI(pending.sessionId, pending.entryId, true);
              }}
            >
              {t(`${key}.rollback.force`)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(`${key}.rollback.delete_title`)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(`${key}.rollback.delete_description`)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingDelete) {
                  void runner.handleDeleteZZMIBackup(
                    pendingDelete.sessionId,
                    pendingDelete.entryId,
                  );
                }
                setPendingDelete(null);
              }}
            >
              {t("g.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={runner.showZZMICleanConfirm} onOpenChange={runner.setShowZZMICleanConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(`${key}.rollback.delete_all_title`)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(`${key}.rollback.delete_all_description`)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void runner.handleDeleteAllZZMIBackups()}>
              {t("g.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function formatBytes(bytes: number) {
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} B`;
}
