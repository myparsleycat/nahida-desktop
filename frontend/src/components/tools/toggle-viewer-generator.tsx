import { Tools } from "@bindings/tools";
import { XXMI } from "@bindings/xxmi";
import { KeyRecorder } from "@renderer/components/mod/key-recorder";
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
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@renderer/components/ui/dialog";
import { Kbd } from "@renderer/components/ui/kbd";
import {
  Section,
  SectionContent,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import { Switch } from "@renderer/components/ui/switch";
import { useSetting } from "@renderer/hooks/use-settings";
import { setSetting } from "@renderer/lib/settings";
import { formatKeyLabel } from "@shared/key-formatter";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import { ArrowRightIcon, FolderSearchIcon } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { ToolLogList } from "./tool-log-list";

const logsQueryKey = ["tools:toggleViewer:logs"] as const;
const stateQueryKey = ["tools:toggleViewer:state"] as const;

export default function ToggleViewerGenerator() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const titleId = useId();
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);

  const { data: xxmiData, isPending: isXXMIPending } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => XXMI.GetXXMIData(),
  });

  const { data: enabled, isPending: isEnabledPending } = useSetting(
    "xxmi.toggleViewerAutoGenerate",
  );
  const { data: hotkey = "ctrl H", isPending: isHotkeyPending } =
    useSetting("xxmi.toggleViewerHotkey");

  const { data: logs = [] } = useQuery<string[]>({
    queryKey: logsQueryKey,
    queryFn: async () => (await Tools.ToggleViewerGetLogs()) ?? [],
  });

  const { data: state } = useQuery({
    queryKey: stateQueryKey,
    queryFn: () => Tools.ToggleViewerGetState(),
  });

  useEffect(() => {
    const offLogs = Events.On("tools:toggle-viewer-logs", (event) => {
      queryClient.setQueryData(logsQueryKey, event.data);
    });
    const offState = Events.On("tools:toggle-viewer-state", (event) => {
      queryClient.setQueryData(stateQueryKey, event.data);
    });

    return () => {
      offLogs();
      offState();
    };
  }, [queryClient]);

  const invalidateState = () => queryClient.invalidateQueries({ queryKey: stateQueryKey });

  const { mutate: setEnabled, isPending: isEnabledSaving } = useMutation({
    mutationFn: (next: boolean) => setSetting("xxmi.toggleViewerAutoGenerate", next),
    onError: (err) => toast.error(err.message),
    onSettled: invalidateState,
  });

  const { mutate: setHotkey, isPending: isHotkeySaving } = useMutation({
    mutationFn: (next: string) => setSetting("xxmi.toggleViewerHotkey", next),
    onError: (err) => toast.error(err.message),
  });

  const { mutate: runBatchGenerate, isPending: isBatchGenerating } = useMutation({
    mutationFn: () => Tools.ToggleViewerRunBatchGenerate(),
    onSuccess: (result) =>
      result.cancelled
        ? toast.info(t("page.tools.toggle_viewer_generator.batch_cancelled"))
        : toast.success(
            t("page.tools.toggle_viewer_generator.batch_generate_done", { count: result.count }),
          ),
    onError: (err) => toast.error(err.message),
    onSettled: invalidateState,
  });

  const { mutate: runBatchDelete, isPending: isBatchDeleting } = useMutation({
    mutationFn: () => Tools.ToggleViewerRunBatchDelete(),
    onSuccess: (result) =>
      result.cancelled
        ? toast.info(t("page.tools.toggle_viewer_generator.batch_cancelled"))
        : toast.success(
            t("page.tools.toggle_viewer_generator.batch_delete_done", { count: result.count }),
          ),
    onError: (err) => toast.error(err.message),
    onSettled: invalidateState,
  });

  const { mutate: stopWork, isPending: isStopping } = useMutation({
    mutationFn: () => Tools.ToggleViewerCancelCurrentWork(),
    onError: (err) => toast.error(err.message),
    onSettled: invalidateState,
  });

  if (isXXMIPending) return null;

  if (!xxmiData?.xxmiPath) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-4 text-center">
        <FolderSearchIcon className="size-8 text-muted-foreground" />
        <div className="space-y-1">
          <h3 className="text-sm font-medium text-foreground">
            {t("page.tools.toggle_viewer_generator.title")}
          </h3>
          <p className="text-sm text-muted-foreground">
            {t("page.tools.toggle_viewer_generator.not_configured")}
          </p>
        </div>
        <Button render={<Link to="/xxmi" />} variant="outline" size="sm">
          {t("page.setting.xxmi.xxmiPath")}
          <ArrowRightIcon />
        </Button>
      </div>
    );
  }

  const mode = state?.isRunning ? state.mode : null;
  const isDeleting = mode === "delete" || isBatchDeleting;
  const isBusy =
    !!state?.isRunning || isBatchGenerating || isBatchDeleting || isStopping || isEnabledSaving;
  const isBatchRunning =
    mode === "generate" || mode === "delete" || isBatchGenerating || isBatchDeleting;
  const statusLabel =
    mode === "generate" || mode === "delete" || mode === "scan"
      ? t(`page.tools.toggle_viewer_generator.status.${mode}`)
      : t("page.tools.toggle_viewer_generator.status.idle");

  return (
    <div className="h-full space-y-6 overflow-y-auto p-4">
      <div className="flex items-start justify-between gap-6">
        <div className="min-w-0 space-y-1">
          <h2 id={titleId} className="text-lg font-semibold text-foreground">
            {t("page.tools.toggle_viewer_generator.auto_generate")}
          </h2>
          <p className="text-sm text-muted-foreground">
            {t("page.tools.toggle_viewer_generator.auto_generate_desc")}
          </p>
        </div>
        <Switch
          aria-labelledby={titleId}
          className="mt-1"
          checked={!!enabled}
          onCheckedChange={(checked) => setEnabled(checked)}
          disabled={isEnabledPending || isEnabledSaving || isDeleting}
        />
      </div>

      <Section>
        <SectionContent>
          <SectionRow title={t("page.tools.toggle_viewer_generator.hotkey")}>
            <HotkeySettingDialog
              value={hotkey}
              disabled={isHotkeyPending || isHotkeySaving || isBatchRunning}
              onSave={(next) => setHotkey(next)}
            />
          </SectionRow>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" disabled={isBusy} onClick={() => runBatchGenerate()}>
              {t("page.tools.toggle_viewer_generator.batch_generate")}
            </Button>
            <Button
              variant="outline"
              disabled={isBusy}
              onClick={() => setIsDeleteConfirmOpen(true)}
            >
              {t("page.tools.toggle_viewer_generator.batch_delete")}
            </Button>
            <Button
              variant="destructive"
              disabled={!state?.isRunning || isStopping}
              onClick={() => stopWork()}
            >
              {t("page.tools.toggle_viewer_generator.stop")}
            </Button>
            <span className="text-xs text-muted-foreground">{statusLabel}</span>
          </div>
        </SectionContent>
      </Section>

      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.tools.toggle_viewer_generator.logs")}</SectionTitle>
        </SectionHeader>
        <SectionContent layout="flow">
          <ToolLogList logs={logs} emptyText={t("page.tools.toggle_viewer_generator.no_logs")} />
        </SectionContent>
      </Section>

      <AlertDialog open={isDeleteConfirmOpen} onOpenChange={setIsDeleteConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("page.tools.toggle_viewer_generator.delete_confirm.title")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.tools.toggle_viewer_generator.delete_confirm.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setIsDeleteConfirmOpen(false);
                runBatchDelete();
              }}
            >
              {t("page.tools.toggle_viewer_generator.batch_delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function HotkeySettingDialog({
  value,
  disabled,
  onSave,
}: {
  value: string;
  disabled: boolean;
  onSave: (value: string) => void;
}) {
  const { t } = useTranslation();
  const labels = value
    .split(" ")
    .map((key) => formatKeyLabel(key))
    .filter((label): label is string => label !== null);

  return (
    <Dialog>
      <DialogTrigger render={<Button variant="outline" size="sm" disabled={disabled} />}>
        <span className="text-xs">{t("page.tools.toggle_viewer_generator.dialog.trigger")}</span>
        {labels.map((label, index) => (
          <Kbd key={`${label}-${index}`}>{label}</Kbd>
        ))}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("page.tools.toggle_viewer_generator.dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("page.tools.toggle_viewer_generator.dialog.description")}
          </DialogDescription>
        </DialogHeader>
        <KeyRecorder defaultValue={value} otherKeys={[]} onSave={onSave} />
      </DialogContent>
    </Dialog>
  );
}
