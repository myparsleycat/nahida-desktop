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
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Switch } from "@renderer/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@renderer/components/ui/tabs";
import type { useModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import { cn } from "@renderer/lib/utils";
import {
  AlertTriangleIcon,
  ChevronRightIcon,
  FolderIcon,
  InfoIcon,
  Loader2Icon,
  RefreshCwIcon,
  TerminalSquareIcon,
} from "lucide-react";
import path from "path-browserify";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

type ModFixRunner = ReturnType<typeof useModFixRunner>;

export function ModFixRunnerDialogs({ runner }: { runner: ModFixRunner }) {
  const { t } = useTranslation();
  const translationKey = "page.mod.dialog.wuwa-fix-runner";
  const aeroEnabled = runner.wuwaOptions.aeroFix !== "none";
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [runner.logs]);

  return (
    <>
      <AlertDialog open={runner.showInstallDialog} onOpenChange={runner.setShowInstallDialog}>
        <AlertDialogContent onClick={(event) => event.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(`${translationKey}.install.title`)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(`${translationKey}.install.description`)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={runner.isPreparing}>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void runner.handleInstallAndContinue();
              }}
              disabled={runner.isPreparing}
            >
              {t(`${translationKey}.install.action`)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={runner.showUpdateDialog} onOpenChange={runner.setShowUpdateDialog}>
        <AlertDialogContent onClick={(event) => event.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(`${translationKey}.update.title`)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(`${translationKey}.update.description`)}
              <br />
              {t(`${translationKey}.update.installed`)}:{" "}
              {runner.prepareResult?.installedVersion ?? t("g.unknown")}
              <br />
              {t(`${translationKey}.update.latest`)}:{" "}
              {runner.prepareResult?.latestVersion ?? t("g.unknown")}
              {runner.isRateLimited && runner.rateResetText ? (
                <>
                  <br />
                  {t(`${translationKey}.update.rate_limited_until`, {
                    time: runner.rateResetText,
                  })}
                </>
              ) : null}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter className="flex gap-2">
            <Button
              variant="outline"
              onClick={() => runner.setShowUpdateDialog(false)}
              disabled={runner.isPreparing}
            >
              {t("g.cancel")}
            </Button>
            <Button
              variant="outline"
              onClick={runner.handleProceedWithoutUpdate}
              disabled={runner.isPreparing}
            >
              {t(`${translationKey}.update.continue_without_update`)}
            </Button>
            <Button
              onClick={() => void runner.handleUpdateAndContinue()}
              disabled={runner.isPreparing}
            >
              {t(`${translationKey}.update.action`)}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={runner.showOptionsDialog} onOpenChange={runner.setShowOptionsDialog}>
        <DialogContent
          onClick={(e) => e.stopPropagation()}
          className="flex max-h-[min(90vh,760px)] min-w-lg flex-col gap-0 overflow-hidden p-0 sm:max-w-xl"
        >
          <DialogHeader className="shrink-0 border-b px-6 py-4">
            <DialogTitle>{t(`${translationKey}.options.title`)}</DialogTitle>
          </DialogHeader>

          <Tabs
            value={runner.optionsTab}
            onValueChange={(value) => runner.setOptionsTab(value as "fix" | "rollback")}
            className="flex min-h-0 flex-1 flex-col gap-0"
          >
            <div className="shrink-0 px-6 pt-4">
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="fix">{t(`${translationKey}.tabs.fix`)}</TabsTrigger>
                <TabsTrigger value="rollback">{t(`${translationKey}.tabs.rollback`)}</TabsTrigger>
              </TabsList>
            </div>

            <TabsContent
              value="fix"
              className="mt-0 flex min-h-0 flex-1 flex-col overflow-hidden outline-none"
            >
              <div className="min-h-0 flex-1 scrollbar-gutter-stable overflow-y-auto">
                <div className="space-y-3 p-4 pr-0.5">
                  <OptionCard
                    disabled={runner.wuwaOptions.stableTexture}
                    checked={runner.wuwaOptions.derivedHashes}
                    onToggle={() =>
                      runner.setOptionFlag("derivedHashes", !runner.wuwaOptions.derivedHashes)
                    }
                    label={t(`${translationKey}.options.derived_hashes.label`)}
                    description={t(`${translationKey}.options.derived_hashes.description`)}
                  />

                  <OptionCard
                    disabled={runner.wuwaOptions.derivedHashes}
                    checked={runner.wuwaOptions.stableTexture}
                    onToggle={() =>
                      runner.setOptionFlag("stableTexture", !runner.wuwaOptions.stableTexture)
                    }
                    label={t(`${translationKey}.options.stable_texture.label`)}
                    description={t(`${translationKey}.options.stable_texture.description`)}
                  />

                  <div
                    role="button"
                    tabIndex={0}
                    onClick={() =>
                      runner.setOptionFlag("rendering33", !runner.wuwaOptions.rendering33)
                    }
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        runner.setOptionFlag("rendering33", !runner.wuwaOptions.rendering33);
                      }
                    }}
                    className="cursor-pointer rounded-lg border bg-card/50 p-4"
                  >
                    <div className="flex items-start gap-4">
                      <div className="min-w-0 flex-1 space-y-1">
                        <div className="text-sm font-semibold">
                          {t(`${translationKey}.options.rendering_33.label`)}
                        </div>
                        {runner.wuwaOptions.rendering33 ? (
                          <p className="text-xs font-medium text-amber-600 dark:text-amber-400">
                            {t(`${translationKey}.options.rendering_33.warning`)}
                          </p>
                        ) : null}
                      </div>
                      <Switch
                        checked={runner.wuwaOptions.rendering33}
                        onCheckedChange={(checked) => runner.setOptionFlag("rendering33", checked)}
                        onClick={(event) => event.stopPropagation()}
                        className="mt-0.5"
                      />
                    </div>
                    {runner.wuwaOptions.rendering33 ? (
                      <div
                        className="mt-3 border-t pt-2.5"
                        onClick={(event) => event.stopPropagation()}
                      >
                        <div className="flex items-start gap-1.5 text-[11px] leading-relaxed text-muted-foreground">
                          <AlertTriangleIcon className="mt-0.5 size-3.5 shrink-0 text-amber-500/80" />
                          <span>{t(`${translationKey}.options.rendering_33.description`)}</span>
                        </div>
                      </div>
                    ) : null}
                  </div>

                  <OptionCard
                    checked={runner.wuwaOptions.aemeathMech}
                    onToggle={() =>
                      runner.setOptionFlag("aemeathMech", !runner.wuwaOptions.aemeathMech)
                    }
                    label={t(`${translationKey}.options.aemeath_mech.label`)}
                    description={t(`${translationKey}.options.aemeath_mech.description`)}
                  />

                  <div
                    role="button"
                    tabIndex={0}
                    onClick={() => runner.toggleAeroFix()}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        runner.toggleAeroFix();
                      }
                    }}
                    className="cursor-pointer rounded-lg border bg-card/50 p-4"
                  >
                    <div className="flex items-start gap-4">
                      <div className="min-w-0 flex-1 space-y-1">
                        <div className="text-sm font-semibold">
                          {t(`${translationKey}.options.aero_fix.label`)}
                        </div>
                        {aeroEnabled ? (
                          <p className="text-xs font-medium text-rose-600 dark:text-rose-400">
                            {t(`${translationKey}.options.aero_fix.warning`)}
                          </p>
                        ) : null}
                      </div>
                      <Switch
                        checked={aeroEnabled}
                        onCheckedChange={() => runner.toggleAeroFix()}
                        onClick={(event) => event.stopPropagation()}
                        className="mt-0.5"
                      />
                    </div>
                    {aeroEnabled ? (
                      <div
                        className="mt-3 space-y-2.5"
                        onClick={(event) => event.stopPropagation()}
                      >
                        <div className="flex rounded-lg border bg-muted/50 p-1">
                          <button
                            type="button"
                            onClick={() => runner.setAeroFixMode("1")}
                            className={cn(
                              "flex-1 rounded-md py-1.5 text-xs font-medium transition-all",
                              runner.wuwaOptions.aeroFix === "1"
                                ? "bg-background text-foreground shadow-sm ring-1 ring-border"
                                : "text-muted-foreground hover:text-foreground",
                            )}
                          >
                            {t(`${translationKey}.options.aero_fix.options.texcoord_override`)}
                          </button>
                          <button
                            type="button"
                            onClick={() => runner.setAeroFixMode("2")}
                            className={cn(
                              "flex-1 rounded-md py-1.5 text-xs font-medium transition-all",
                              runner.wuwaOptions.aeroFix === "2"
                                ? "bg-background text-foreground shadow-sm ring-1 ring-border"
                                : "text-muted-foreground hover:text-foreground",
                            )}
                          >
                            {t(`${translationKey}.options.aero_fix.options.texture_mirror_flip`)}
                          </button>
                        </div>
                        <div className="flex items-start gap-1.5 text-[11px] leading-relaxed text-muted-foreground">
                          <InfoIcon className="mt-0.5 size-3.5 shrink-0" />
                          <span>{t(`${translationKey}.options.aero_fix.tip`)}</span>
                        </div>
                      </div>
                    ) : null}
                  </div>
                </div>
              </div>
            </TabsContent>

            <TabsContent
              value="rollback"
              className="mt-0 flex min-h-0 flex-1 flex-col overflow-hidden outline-none"
            >
              <div className="flex shrink-0 items-center justify-between gap-3 border-b px-6 py-3">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void runner.refreshBackups()}
                  disabled={runner.isLoadingBackups || runner.isRollbackBusy}
                >
                  {runner.isLoadingBackups ? (
                    <Loader2Icon className="size-3.5 animate-spin" />
                  ) : (
                    <RefreshCwIcon className="size-3.5" />
                  )}
                  {t(`${translationKey}.rollback.refresh`)}
                </Button>
                {runner.backupSize.count > 0 ? (
                  <div className="text-xs text-muted-foreground">
                    <span className="font-semibold">
                      {t(`${translationKey}.rollback.backup_size_label`)}
                    </span>{" "}
                    <span className="rounded border bg-muted px-1.5 py-0.5 font-mono font-bold text-foreground">
                      {formatBackupSize(runner.backupSize.bytes)}
                    </span>
                  </div>
                ) : null}
              </div>

              <div className="min-h-0 flex-1 scrollbar-gutter-stable overflow-y-auto">
                <div className="space-y-3 p-4 pr-0.5">
                  {runner.isLoadingBackups ? (
                    <div className="animate-pulse py-10 text-center text-sm text-muted-foreground">
                      {t(`${translationKey}.rollback.scanning`)}
                    </div>
                  ) : runner.backupGroups.length === 0 ? (
                    <div className="flex flex-col items-center justify-center gap-2 py-12 text-muted-foreground">
                      <InfoIcon className="size-10 opacity-20" />
                      <span className="text-xs font-medium">
                        {t(`${translationKey}.rollback.no_backups`)}
                      </span>
                    </div>
                  ) : (
                    runner.backupGroups.map((group, index) => (
                      <BackupGroupCard
                        key={group.groupKey}
                        group={group}
                        index={index}
                        basePath={runner.activeModPath ?? ""}
                        pending={runner.pendingRollbackKey === group.groupKey}
                        busy={runner.isRollbackBusy}
                        translationKey={translationKey}
                        onRequestRestore={() => runner.setPendingRollbackKey(group.groupKey)}
                        onCancel={() => runner.setPendingRollbackKey(null)}
                        onConfirm={() => void runner.handleRollbackToGroup(group.groupKey)}
                      />
                    ))
                  )}
                </div>
              </div>

              <div className="shrink-0 border-t">
                <button
                  type="button"
                  onClick={() => runner.setShowAdvancedRollback(!runner.showAdvancedRollback)}
                  className="flex w-full items-center gap-2 px-6 py-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
                >
                  <ChevronRightIcon
                    className={cn(
                      "size-3 transition-transform",
                      runner.showAdvancedRollback && "rotate-90",
                    )}
                  />
                  <span className="font-semibold">{t(`${translationKey}.rollback.advanced`)}</span>
                  <span className="ml-auto text-[10px] opacity-60">
                    {t(`${translationKey}.rollback.advanced_hint`)}
                  </span>
                </button>

                {runner.showAdvancedRollback ? (
                  <div className="space-y-3 bg-muted/30 px-6 pt-1 pb-5">
                    <div className="flex items-start gap-3 rounded-lg border bg-card p-3">
                      <div className="min-w-0 flex-1">
                        <p className="text-xs font-bold">
                          {t(`${translationKey}.rollback.restore_all`)}
                        </p>
                        <p className="mt-0.5 text-[10px] leading-relaxed text-muted-foreground">
                          {t(`${translationKey}.rollback.restore_all_desc`)}
                        </p>
                      </div>
                      {runner.backupGroups.length > 0 ? (
                        runner.pendingRollbackKey === "__RESTORE_ALL__" ? (
                          <div className="flex flex-col items-end gap-1.5">
                            <span className="text-[10px] font-bold text-rose-500">
                              {t(`${translationKey}.rollback.warn_delete_all`, {
                                n: runner.backupGroups.length,
                              })}
                            </span>
                            <div className="flex gap-1.5">
                              <Button
                                size="sm"
                                variant="destructive"
                                className="h-7 px-2 text-[10px]"
                                disabled={runner.isRollbackBusy}
                                onClick={() => void runner.handleRollbackToGroup("__RESTORE_ALL__")}
                              >
                                {t("g.confirm")}
                              </Button>
                              <Button
                                size="sm"
                                variant="outline"
                                className="h-7 px-2 text-[10px]"
                                disabled={runner.isRollbackBusy}
                                onClick={() => runner.setPendingRollbackKey(null)}
                              >
                                {t("g.cancel")}
                              </Button>
                            </div>
                          </div>
                        ) : (
                          <Button
                            size="sm"
                            variant="outline"
                            className="h-8 border-amber-400/40 text-[11px] font-bold text-amber-600 dark:text-amber-400"
                            disabled={runner.isRollbackBusy}
                            onClick={() => runner.setPendingRollbackKey("__RESTORE_ALL__")}
                          >
                            {t(`${translationKey}.rollback.restore_all_action`)}
                          </Button>
                        )
                      ) : (
                        <span className="self-center text-[10px] text-muted-foreground italic">
                          {t(`${translationKey}.rollback.no_backups_short`)}
                        </span>
                      )}
                    </div>

                    <div className="flex items-start gap-3 rounded-lg border border-rose-200/50 bg-rose-50/40 p-3 dark:border-rose-900/40 dark:bg-rose-950/10">
                      <div className="min-w-0 flex-1">
                        <p className="text-xs font-bold">
                          {t(`${translationKey}.rollback.clean_backups`)}
                        </p>
                        <p className="mt-0.5 text-[10px] leading-relaxed text-muted-foreground">
                          {t(`${translationKey}.rollback.clean_backups_desc`)}
                        </p>
                      </div>
                      {runner.backupSize.count > 0 ? (
                        runner.showCleanConfirm ? (
                          <div className="flex flex-col items-end gap-1.5">
                            <span className="text-[10px] font-bold text-rose-500">
                              {t(`${translationKey}.rollback.warn_clean`)}
                            </span>
                            <div className="flex items-center gap-1.5">
                              <Input
                                value={runner.cleanConfirmInput}
                                onChange={(event) =>
                                  runner.setCleanConfirmInput(event.target.value)
                                }
                                placeholder="WIPE"
                                className="h-7 w-16 px-1.5 text-center font-mono text-[10px] font-bold uppercase"
                                disabled={runner.isRollbackBusy}
                              />
                              <Button
                                size="sm"
                                variant="destructive"
                                className="h-7 px-2 text-[10px]"
                                disabled={
                                  runner.isRollbackBusy ||
                                  runner.cleanConfirmInput.toUpperCase() !== "WIPE"
                                }
                                onClick={() => void runner.handleCleanBackups()}
                              >
                                {t("g.confirm")}
                              </Button>
                              <Button
                                size="sm"
                                variant="outline"
                                className="h-7 px-2 text-[10px]"
                                disabled={runner.isRollbackBusy}
                                onClick={() => {
                                  runner.setShowCleanConfirm(false);
                                  runner.setCleanConfirmInput("");
                                }}
                              >
                                {t("g.cancel")}
                              </Button>
                            </div>
                          </div>
                        ) : (
                          <Button
                            size="sm"
                            variant="outline"
                            className="h-8 border-rose-400/40 text-[11px] font-bold text-rose-600 dark:text-rose-400"
                            disabled={runner.isRollbackBusy}
                            onClick={() => runner.setShowCleanConfirm(true)}
                          >
                            {t(`${translationKey}.rollback.clean_action`)}
                          </Button>
                        )
                      ) : (
                        <span className="self-center text-[10px] text-muted-foreground italic">
                          {t(`${translationKey}.rollback.no_backups_short`)}
                        </span>
                      )}
                    </div>
                  </div>
                ) : null}
              </div>
            </TabsContent>
          </Tabs>

          <DialogFooter className="mx-0 mb-0 shrink-0 border-t px-6 py-4">
            <Button variant="outline" onClick={() => runner.setShowOptionsDialog(false)}>
              {t("g.cancel")}
            </Button>
            <Button onClick={() => void runner.handleRunWuwaFixer()}>
              {t(`${translationKey}.options.run`)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={runner.showLogModal}
        onOpenChange={(nextOpen, eventDetails) => {
          if (nextOpen) {
            runner.setShowLogModal(true);
            queueMicrotask(() => inputRef.current?.focus());
            return;
          }

          if (eventDetails.reason === "escape-key" && runner.isRunning) {
            eventDetails.cancel();
            runner.handleCancel();
            return;
          }
          runner.setShowLogModal(false);
        }}
      >
        <AlertDialogContent onClick={(event) => event.stopPropagation()} className="min-w-xl">
          <AlertDialogHeader>
            <AlertDialogTitle>{runner.labels.logTitle}</AlertDialogTitle>
          </AlertDialogHeader>
          <ScrollArea
            viewportRef={scrollRef}
            className="h-[calc(100vh-430px)] w-full rounded-md border bg-muted font-mono text-xs break-all whitespace-pre-wrap"
          >
            <div className="space-y-2 p-3">
              {runner.logs.map((log, index) => (
                <div key={`log-${index.toString()}`} className="flex w-full flex-row space-x-1">
                  <ChevronRightIcon className="size-4 shrink-0" />
                  <div
                    className={cn(
                      log.toLowerCase().includes("complete") && "text-green-500",
                      log.toLowerCase().includes("error") && "text-red-500",
                      log.toLowerCase().includes("warning") && "text-yellow-500",
                    )}
                  >
                    {log}
                  </div>
                </div>
              ))}
              {runner.isRunning && (
                <div className="animate-pulse text-primary">{t("page.mod.log-dialog.running")}</div>
              )}
            </div>
          </ScrollArea>
          <div className="flex gap-2">
            <Input
              ref={inputRef}
              placeholder={t(`${translationKey}.log.input_placeholder`)}
              value={runner.inputCmd}
              disabled={!runner.isRunning}
              onChange={(event) => runner.setInputCmd(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && runner.isRunning) {
                  runner.handleSendInput();
                }
              }}
            />
            <Button
              variant="outline"
              size="icon"
              className="shrink-0"
              onClick={runner.handleSendInput}
              disabled={!runner.isRunning}
            >
              <TerminalSquareIcon className="size-4" />
            </Button>
          </div>
          <AlertDialogFooter>
            {runner.isRunning ? (
              <Button variant="destructive" onClick={runner.handleCancel}>
                {t("g.cancel")}
              </Button>
            ) : (
              <Button onClick={() => runner.setShowLogModal(false)}>{t("g.close")}</Button>
            )}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function OptionCard({
  checked,
  onToggle,
  label,
  description,
  disabled = false,
}: {
  checked: boolean;
  onToggle: () => void;
  label: string;
  description: string;
  disabled?: boolean;
}) {
  return (
    <div
      role="button"
      tabIndex={disabled ? -1 : 0}
      onClick={() => {
        if (!disabled) {
          onToggle();
        }
      }}
      onKeyDown={(event) => {
        if (disabled) {
          return;
        }
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onToggle();
        }
      }}
      className={cn(
        "flex cursor-pointer items-start gap-4 rounded-lg border bg-card/50 p-4",
        disabled && "pointer-events-none opacity-50",
      )}
    >
      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="text-sm font-semibold">{label}</div>
        <p className="text-[11px] leading-relaxed text-muted-foreground">{description}</p>
      </div>
      <Switch
        checked={checked}
        disabled={disabled}
        onCheckedChange={() => onToggle()}
        onClick={(event) => event.stopPropagation()}
        className="mt-0.5"
      />
    </div>
  );
}

function BackupGroupCard({
  group,
  index,
  basePath,
  pending,
  busy,
  translationKey,
  onRequestRestore,
  onCancel,
  onConfirm,
}: {
  group: ModFixRunner["backupGroups"][number];
  index: number;
  basePath: string;
  pending: boolean;
  busy: boolean;
  translationKey: string;
  onRequestRestore: () => void;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useTranslation();
  const filesByDir = groupFilesByDir(group.files, basePath);

  return (
    <div
      className={cn(
        "overflow-hidden rounded-[10px] border bg-card/60 shadow-sm transition-all",
        pending && "border-amber-400/80 ring-1 ring-amber-400/20 dark:border-amber-500/50",
      )}
    >
      <div className="flex flex-col gap-3 p-4">
        <div className="flex flex-col justify-between gap-3 border-b pb-2.5 sm:flex-row sm:items-center">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="rounded border border-sky-200/50 bg-sky-50 px-2 py-0.5 font-mono text-[10px] font-bold text-sky-600 dark:border-sky-500/20 dark:bg-sky-950/40 dark:text-sky-400">
              {formatGroupKey(group.groupKey)}
            </span>
            <span className="rounded border bg-muted px-1.5 py-0.5 text-[9px] font-bold text-muted-foreground">
              {t(`${translationKey}.rollback.file_count`, { n: (group.files ?? []).length })}
            </span>
            {index === 0 ? (
              <span className="rounded border border-emerald-200/40 bg-emerald-500/15 px-1.5 py-0.5 text-[9px] font-black tracking-wider text-emerald-700 uppercase dark:border-emerald-500/10 dark:text-emerald-400">
                {t(`${translationKey}.rollback.latest_state`)}
              </span>
            ) : null}
          </div>

          <div className="flex shrink-0 items-center justify-end gap-2">
            {pending ? (
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="destructive"
                  className="h-8 text-[11px]"
                  disabled={busy}
                  onClick={onConfirm}
                >
                  {t(`${translationKey}.rollback.confirm_restore`)}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="h-8 text-[11px]"
                  disabled={busy}
                  onClick={onCancel}
                >
                  {t("g.cancel")}
                </Button>
              </div>
            ) : (
              <Button
                size="sm"
                variant="outline"
                className="h-8 text-[11px]"
                disabled={busy}
                onClick={onRequestRestore}
              >
                {t(`${translationKey}.rollback.restore`)}
              </Button>
            )}
          </div>
        </div>

        <div className="max-h-[150px] space-y-2.5 overflow-y-auto rounded-lg border bg-muted/40 p-3 shadow-inner">
          {[...filesByDir.entries()].map(([dir, files]) => (
            <div key={dir} className="flex flex-col font-mono text-[10.5px] leading-normal">
              <span className="mb-0.5 flex items-center gap-1 font-bold break-all text-sky-600 dark:text-sky-400">
                <FolderIcon className="size-3 shrink-0 text-sky-500/80" />
                {dir}/
              </span>
              <span className="pl-4 break-all text-muted-foreground">{files.join(", ")}</span>
            </div>
          ))}
        </div>

        {pending ? (
          <div className="flex items-start gap-2 rounded-lg border border-amber-500/20 bg-amber-500/5 p-2.5 text-[11px] text-amber-600 dark:text-amber-400">
            <AlertTriangleIcon className="mt-0.5 size-4 shrink-0 text-amber-500" />
            <div className="flex-1 leading-relaxed font-bold">
              {t(`${translationKey}.rollback.warn_restore`)}
              {index > 0 ? (
                <span className="mt-0.5 block text-[9px] font-medium text-amber-500/90 dark:text-amber-400/80">
                  {t(`${translationKey}.rollback.warn_clean_newer`, { n: index })}
                </span>
              ) : null}
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}

function formatGroupKey(key: string) {
  return key.replace(/ (\d{2})-(\d{2})$/, " $1:$2");
}

function formatBackupSize(bytes: number) {
  if (bytes > 1048576) {
    return `${(bytes / 1048576).toFixed(1)} MB`;
  }
  return `${(bytes / 1024).toFixed(1)} KB`;
}

function groupFilesByDir(files: ModFixRunner["backupGroups"][number]["files"], basePath: string) {
  const map = new Map<string, string[]>();
  const normalizedBase = basePath.replace(/[\\/]+$/, "");

  for (const file of files ?? []) {
    const relative = file.originalPath.startsWith(normalizedBase)
      ? file.originalPath.slice(normalizedBase.length).replace(/^[\\/]+/, "")
      : file.originalPath;
    const dirName = path.dirname(relative).replace(/\\/g, "/");
    const fileName = path.basename(relative);
    const key = dirName === "." ? "." : dirName;
    const list = map.get(key) ?? [];
    list.push(fileName);
    map.set(key, list);
  }

  return map;
}
