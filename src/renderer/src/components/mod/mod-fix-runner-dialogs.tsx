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
import { Checkbox } from "@renderer/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Label } from "@renderer/components/ui/label";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import type { useModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import { cn } from "@renderer/lib/utils";
import { ChevronRightIcon, TerminalSquareIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

type ModFixRunner = ReturnType<typeof useModFixRunner>;

export function ModFixRunnerDialogs({ runner }: { runner: ModFixRunner }) {
  const { t } = useTranslation();
  const rollbackEnabled = runner.wuwaOptions.rollback;
  const translationKey = "page.mod.dialog.wuwa-fix-runner";

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
        <DialogContent onClick={(e) => e.stopPropagation()} className="min-w-lg">
          <DialogHeader>
            <DialogTitle>{t(`${translationKey}.options.title`)}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex items-center justify-between rounded-md border p-3">
              <div className="space-y-1">
                <Label htmlFor="wuwa-rollback">{t(`${translationKey}.options.rollback`)}</Label>
              </div>
              <Checkbox
                id="wuwa-rollback"
                checked={runner.wuwaOptions.rollback}
                onCheckedChange={(checked) => runner.setCheckbox("rollback", checked === true)}
              />
            </div>

            <div className={cn("grid gap-3", rollbackEnabled && "opacity-50")}>
              <div className="rounded-md border p-3">
                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="wuwa-derived"
                    checked={runner.wuwaOptions.derivedHashes}
                    disabled={rollbackEnabled}
                    onCheckedChange={(checked) =>
                      runner.setCheckbox("derivedHashes", checked === true)
                    }
                  />
                  <Label htmlFor="wuwa-derived">
                    {t(`${translationKey}.options.derived_hashes.label`)}
                  </Label>
                </div>

                <p className="pl-6 text-xs text-muted-foreground">
                  {t(`${translationKey}.options.derived_hashes.description`)}
                </p>
              </div>

              <div className="rounded-md border p-3">
                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="wuwa-stable"
                    checked={runner.wuwaOptions.stableTexture}
                    disabled={rollbackEnabled}
                    onCheckedChange={(checked) =>
                      runner.setCheckbox("stableTexture", checked === true)
                    }
                  />
                  <Label htmlFor="wuwa-stable">
                    {t(`${translationKey}.options.stable_texture.label`)}
                  </Label>
                </div>

                <p className="pl-6 text-xs text-muted-foreground">
                  {t(`${translationKey}.options.stable_texture.description`)}
                </p>
              </div>

              <div className="rounded-md border p-3">
                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="wuwa-mech"
                    checked={runner.wuwaOptions.aemeathMech}
                    disabled={rollbackEnabled}
                    onCheckedChange={(checked) =>
                      runner.setCheckbox("aemeathMech", checked === true)
                    }
                  />
                  <Label htmlFor="wuwa-mech">
                    {t(`${translationKey}.options.aemeath_mech.label`)}
                  </Label>
                </div>

                <p className="pl-6 text-xs text-muted-foreground">
                  {t(`${translationKey}.options.aemeath_mech.description`)}
                </p>
              </div>

              <div className="flex flex-col rounded-md border p-3">
                <div className="flex items-center justify-between w-full">
                  <div className="flex flex-col space-y-1">
                    <Label>{t(`${translationKey}.options.aero_fix.label`)}</Label>
                    <p className="text-xs text-muted-foreground">
                      {t(`${translationKey}.options.aero_fix.description`)}
                    </p>
                  </div>

                  <Select
                    value={runner.wuwaOptions.aeroFix}
                    onValueChange={(value) =>
                      runner.setWuwaOptions((prev) => ({
                        ...prev,
                        aeroFix: value as "none" | "1" | "2",
                      }))
                    }
                    disabled={rollbackEnabled}
                  >
                    <SelectTrigger className="w-44">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      <SelectGroup>
                        <SelectItem value="none">
                          {t(`${translationKey}.options.aero_fix.options.none`)}
                        </SelectItem>
                        <SelectItem value="1">
                          {t(`${translationKey}.options.aero_fix.options.texcoord_override`)}
                        </SelectItem>
                        <SelectItem value="2">
                          {t(`${translationKey}.options.aero_fix.options.texture_mirror_flip`)}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>

                {runner.wuwaOptions.aeroFix !== "none" && (
                  <p className="text-xs text-destructive">
                    {t(`${translationKey}.options.aero_fix.warning`)}
                  </p>
                )}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => runner.setShowOptionsDialog(false)}>
              {t("g.cancel")}
            </Button>
            <Button onClick={() => void runner.handleRunWuwaFixer()}>
              {t(`${translationKey}.options.run`)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={runner.showLogModal} onOpenChange={runner.setShowLogModal}>
        <AlertDialogContent
          onEscapeKeyDown={(event) => {
            if (runner.isRunning) {
              event.preventDefault();
              runner.handleCancel();
            }
          }}
          onClick={(event) => event.stopPropagation()}
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            runner.inputRef.current?.focus();
          }}
          className="min-w-xl"
        >
          <AlertDialogHeader>
            <AlertDialogTitle>{runner.labels.logTitle}</AlertDialogTitle>
          </AlertDialogHeader>
          <ScrollArea
            viewportRef={runner.scrollRef}
            className="h-[calc(100vh-430px)] w-full rounded-md border bg-muted font-mono text-xs whitespace-pre-wrap break-all"
          >
            <div className="p-3 space-y-2">
              {runner.logs.map((log, index) => (
                <div key={`log-${index.toString()}`} className="flex flex-row space-x-1 w-full">
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
              ref={runner.inputRef}
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
