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
import { Switch } from "@renderer/components/ui/switch";
import { formatKeyLabel } from "@shared/key-formatter";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { KeyRecorder } from "../mod/key-recorder";
import { ScrollArea } from "../ui/scroll-area";

function KeyDisplay({ keys }: { keys: string }) {
  return (
    <div className="flex flex-wrap items-center gap-1">
      {keys
        .split(" ")
        .map((k) => formatKeyLabel(k))
        .filter((k): k is string => k !== null)
        .map((label, idx) => (
          <Kbd key={idx.toString()} className="text-xs">
            {label}
          </Kbd>
        ))}
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
  onSave: (newValue: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <Dialog>
      <DialogTrigger
        className="flex flex-row items-center w-full transition-colors border border-white/20 space-x-1 bg-foreground/5 hover:bg-background/10 p-2 rounded-lg justify-start disabled:pointer-events-none disabled:opacity-60"
        onClick={(e) => e.stopPropagation()}
        disabled={disabled}
      >
        <span className="text-sm">{t("page.tools.toggle_viewer_generator.dialog.trigger")}</span>
        <KeyDisplay keys={value} />
      </DialogTrigger>
      <DialogContent onClick={(e) => e.stopPropagation()}>
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

export default function ToggleViewerGenerator() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const { data: xxmiData } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => window.api.invoke("xxmi:getXXMIData"),
  });

  const { data: enabled, isPending: isQueryPending } = useQuery({
    queryKey: ["setting:xxmi:getToggleViewerAutoGenerate"],
    queryFn: () => window.api.invoke("setting:xxmi:getToggleViewerAutoGenerate"),
  });
  const { data: hotkey = "ctrl H", isPending: isHotkeyPending } = useQuery({
    queryKey: ["setting:xxmi:getToggleViewerHotkey"],
    queryFn: () => window.api.invoke("setting:xxmi:getToggleViewerHotkey"),
  });

  const { data: logs = [] } = useQuery<string[]>({
    queryKey: ["setting:xxmi:getToggleViewerLogs"],
    queryFn: () => window.api.invoke("setting:xxmi:getToggleViewerLogs"),
  });

  const { data: state } = useQuery<{ isRunning: boolean; mode: string | null }>({
    queryKey: ["setting:xxmi:getToggleViewerState"],
    queryFn: () => window.api.invoke("setting:xxmi:getToggleViewerState"),
    refetchInterval: 800,
  });

  useEffect(() => {
    const unsubscribe = window.api.on("setting:xxmi:toggleViewerLogs", (nextLogs) => {
      queryClient.setQueryData(["setting:xxmi:getToggleViewerLogs"], nextLogs);
    });

    return unsubscribe;
  }, [queryClient]);

  const { mutate, isPending: isMutatePending } = useMutation({
    mutationFn: (newEnabled: boolean) =>
      window.api.invoke("setting:xxmi:setToggleViewerAutoGenerate", newEnabled),
    onSuccess: (_, newEnabled) => {
      queryClient.setQueryData(["setting:xxmi:getToggleViewerAutoGenerate"], newEnabled);
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerState"] });
    },
    onError: (err) => {
      toast.error(err.message);
    },
  });

  const { mutate: runBatchGenerate, isPending: isBatchGenerating } = useMutation({
    mutationFn: () => window.api.invoke("setting:xxmi:runToggleViewerBatchGenerate"),
    onError: (err) => {
      toast.error(err.message);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerState"] });
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerLogs"] });
    },
  });

  const { mutate: runBatchDelete, isPending: isBatchDeleting } = useMutation({
    mutationFn: () => window.api.invoke("setting:xxmi:runToggleViewerBatchDelete"),
    onMutate: () => {
      queryClient.setQueryData(["setting:xxmi:getToggleViewerAutoGenerate"], false);
    },
    onError: (err) => {
      toast.error(err.message);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerAutoGenerate"] });
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerState"] });
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerLogs"] });
    },
  });

  const { mutate: stopWork, isPending: isStopping } = useMutation({
    mutationFn: () => window.api.invoke("setting:xxmi:cancelToggleViewerWork"),
    onError: (err) => {
      toast.error(err.message);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerState"] });
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerLogs"] });
    },
  });

  const { mutate: setHotkey, isPending: isHotkeySaving } = useMutation({
    mutationFn: (newHotkey: string) =>
      window.api.invoke("setting:xxmi:setToggleViewerHotkey", newHotkey),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerHotkey"] });
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerLogs"] });
      queryClient.invalidateQueries({ queryKey: ["setting:xxmi:getToggleViewerState"] });
    },
    onError: (err) => {
      toast.error(err.message);
    },
  });

  const isRunning = !!state?.isRunning;
  const mode = state?.mode ?? null;
  const isGenerating = mode === "generate";
  const isDeleting = mode === "delete";
  const isBusy =
    isMutatePending ||
    isBatchGenerating ||
    isBatchDeleting ||
    isStopping ||
    isQueryPending ||
    isHotkeyPending ||
    isHotkeySaving;
  const isSwitchBusy = isQueryPending || isMutatePending || isDeleting;

  if (!xxmiData?.xxmiPath) {
    return (
      <div className="flex flex-col items-center justify-center w-full p-2 text-center">
        <h3 className="text-lg font-semibold text-muted-foreground">
          {t("page.tools.toggle_viewer_generator.title")}
        </h3>
        <p className="text-sm text-muted-foreground italic">
          {t("page.tools.toggle_viewer_generator.not_configured")}
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col space-y-4 p-4">
      <div className="flex flex-row items-center justify-between w-full p-3 rounded-lg border hover:shadow transition-shadow duration-200">
        <div className="flex flex-col space-y-1">
          <h3 className="text font-semibold">
            {t("page.tools.toggle_viewer_generator.auto_generate")}
          </h3>
          <p className="text-sm text-muted-foreground">
            {t("page.tools.toggle_viewer_generator.auto_generate_desc")}
          </p>
        </div>
        <Switch
          checked={!!enabled}
          onCheckedChange={(checked) => mutate(checked)}
          disabled={isSwitchBusy}
        />
      </div>

      <div className="flex flex-row items-center justify-between w-full p-3 rounded-lg border hover:shadow transition-shadow duration-200 gap-2">
        <div className="flex flex-col space-y-1 shrink-0">
          <h3 className="text font-semibold">{t("page.tools.toggle_viewer_generator.hotkey")}</h3>
        </div>
        <div className="w-full max-w-md">
          <HotkeySettingDialog
            value={hotkey}
            disabled={isHotkeyPending || isHotkeySaving || isGenerating || isDeleting}
            onSave={(newValue) => setHotkey(newValue)}
          />
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          disabled={isBusy || isRunning || isGenerating || isDeleting}
          onClick={() => runBatchGenerate()}
        >
          {t("page.tools.toggle_viewer_generator.batch_generate")}
        </Button>
        <Button
          variant="outline"
          disabled={isBusy || isRunning || isGenerating || isDeleting}
          onClick={() => runBatchDelete()}
        >
          {t("page.tools.toggle_viewer_generator.batch_delete")}
        </Button>
        <Button
          variant="destructive"
          disabled={!isRunning || isStopping}
          onClick={() => stopWork()}
        >
          {t("page.tools.toggle_viewer_generator.stop")}
        </Button>
        <div className="text-xs text-muted-foreground self-center">
          {isRunning
            ? t("page.tools.toggle_viewer_generator.running", {
                mode: state?.mode || t("page.tools.toggle_viewer_generator.unknown"),
              })
            : t("page.tools.toggle_viewer_generator.idle")}
        </div>
      </div>

      <div className="flex flex-col w-full p-3 rounded-lg border hover:shadow transition-shadow duration-200 h-80">
        <div className="text-sm font-medium text-muted-foreground mb-2">
          {t("page.tools.toggle_viewer_generator.logs")}
        </div>
        <ScrollArea className="flex-1 overflow-auto rounded border bg-muted/30 p-2">
          {logs.length === 0 ? (
            <div className="text-sm text-muted-foreground italic">
              {t("page.tools.toggle_viewer_generator.no_logs")}
            </div>
          ) : (
            <div className="flex flex-col gap-1">
              {[...logs].reverse().map((log, index) => (
                <div key={`${log}-${index}`} className="text-xs font-mono break-all">
                  {log}
                </div>
              ))}
            </div>
          )}
        </ScrollArea>
      </div>
    </div>
  );
}
