import { Mod, type CompressionState } from "@bindings/mod";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import { Progress } from "@renderer/components/ui/progress";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import { useModCompressionState } from "@renderer/hooks/use-mod-compression-state";
import { Logger } from "@renderer/lib/logger";
import { formatSize } from "@shared/utils";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const activeStatuses = new Set(["checking", "compressing", "decompressing"]);

function compressionProgress(state: CompressionState) {
  if (state.totalBytes > 0) return Math.min(100, (state.processedBytes / state.totalBytes) * 100);
  if (state.totalFiles > 0) return Math.min(100, (state.processedFiles / state.totalFiles) * 100);
  return 0;
}

export function ModCompressionCard() {
  const { t } = useTranslation();
  const [state, setState] = useModCompressionState();
  const [threshold, setThreshold] = useState<string | null>(null);
  const [requestPending, setRequestPending] = useState(false);
  const requestPendingRef = useRef(false);

  if (!state) return null;

  const running = activeStatuses.has(state.status);
  const checked = running && state.targetEnabled != null ? state.targetEnabled : state.enabled;
  const progress = compressionProgress(state);

  const updateConfig = async (method: string, thresholdMiB: number) => {
    if (requestPendingRef.current) return;
    requestPendingRef.current = true;
    setRequestPending(true);
    try {
      setState(await Mod.SetCompressionConfig({ method, thresholdMiB }));
    } catch (error) {
      Logger.error(error, "ModCompressionCard:updateConfig");
      toast.error(t("page.setting.mod.compression.saveFailed"));
    } finally {
      requestPendingRef.current = false;
      setRequestPending(false);
    }
  };

  const commitThreshold = () => {
    const value = Number(threshold ?? state.thresholdMiB);
    const next = Number.isInteger(value) ? Math.min(64, Math.max(1, value)) : state.thresholdMiB;
    setThreshold(null);
    if (next !== state.thresholdMiB) void updateConfig(state.method, next);
  };

  const toggle = async (enabled: boolean) => {
    if (requestPendingRef.current) return;
    requestPendingRef.current = true;
    setRequestPending(true);
    try {
      setState(await Mod.SetCompressionEnabled(enabled));
    } catch (error) {
      Logger.error(error, "ModCompressionCard:toggle");
      toast.error(t("page.setting.mod.compression.toggleFailed"));
    } finally {
      requestPendingRef.current = false;
      setRequestPending(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          {t("page.setting.mod.compression.title")}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center justify-between gap-4">
          <div className="space-y-0.5">
            <div className="text-sm font-medium">{t("page.setting.mod.compression.method")}</div>
            <p className="text-xs text-muted-foreground">
              {t(`page.setting.mod.compression.methods.${state.method}.description`)}
            </p>
          </div>
          <Select
            value={state.method}
            disabled={!state.canConfigure || requestPending}
            onValueChange={(method) => {
              if (method) void updateConfig(method, state.thresholdMiB);
            }}
          >
            <SelectTrigger className="w-40" aria-label={t("page.setting.mod.compression.method")}>
              <SelectValue>{state.method === "zstd" ? "Zstd" : "XPRESS4K"}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="zstd">Zstd</SelectItem>
              <SelectItem value="xpress4k">XPRESS4K</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {state.method === "zstd" && (
          <div className="flex items-center justify-between gap-4">
            <div className="space-y-0.5">
              <label className="text-sm font-medium" htmlFor="mod-compression-threshold">
                {t("page.setting.mod.compression.threshold")}
              </label>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.mod.compression.thresholdDescription")}
              </p>
            </div>
            <div className="flex items-center gap-2">
              <Input
                id="mod-compression-threshold"
                className="w-24"
                type="number"
                min={1}
                max={64}
                step={1}
                value={threshold ?? String(state.thresholdMiB)}
                disabled={!state.canConfigure || requestPending}
                onChange={(event) => setThreshold(event.target.value)}
                onBlur={commitThreshold}
                onKeyDown={(event) => {
                  if (event.key === "Enter") event.currentTarget.blur();
                }}
              />
              <span className="text-sm text-muted-foreground">MiB</span>
            </div>
          </div>
        )}

        <div className="rounded-md border p-3">
          <div className="flex items-center justify-between gap-4">
            <div className="space-y-1">
              <div className="text-sm font-medium">
                {t(`page.setting.mod.compression.status.${state.status}`)}
              </div>
              {state.currentFileName && (
                <p className="max-w-96 truncate text-xs text-muted-foreground">
                  {state.currentFileName}
                </p>
              )}
            </div>
            <Switch
              checked={checked}
              disabled={!state.canToggle || requestPending}
              aria-label={t("page.setting.mod.compression.toggle")}
              onCheckedChange={(value) => void toggle(value)}
            />
          </div>

          {running && (
            <div className="mt-3 space-y-1.5">
              <Progress value={progress} />
              <div className="flex justify-between text-xs text-muted-foreground">
                <span>
                  {t("page.setting.mod.compression.files", {
                    processed: state.processedFiles,
                    total: state.totalFiles,
                  })}
                </span>
                <span>
                  {formatSize(state.processedBytes)} / {formatSize(state.totalBytes)} ·{" "}
                  {Math.round(progress)}%
                </span>
              </div>
            </div>
          )}

          {state.status === "error" && (
            <p className="mt-2 text-xs text-destructive">
              {t("page.setting.mod.compression.error")}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
