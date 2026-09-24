import { Backup } from "@bindings/backup";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Separator } from "@renderer/components/ui/separator";
import { Switch } from "@renderer/components/ui/switch";
import { useSettings } from "@renderer/hooks/use-settings";
import type { BackupInterval } from "@shared/settings";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { backupErrorMessage } from "./errors";
import { backupOverviewKey } from "./queries";

const settingsConfig = {
  enabled: "backup.enabled",
  interval: "backup.interval",
  onStartup: "backup.onStartup",
  watchChanges: "backup.watchChanges",
  keepCount: "backup.keepCount",
} as const;

const INTERVALS: BackupInterval[] = ["6h", "12h", "24h", "7d"];

function Row({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-6">
      <div className="space-y-0.5">
        <span className="text-sm font-medium">{title}</span>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      {children}
    </div>
  );
}

export function BackupScheduleCard({ deviceName }: { deviceName: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { settings, update, isLoading } = useSettings(settingsConfig);
  // The inputs hold a draft only while the user is editing; otherwise they show
  // the stored value.
  const [nameDraft, setNameDraft] = useState<string | null>(null);
  const [keepCountDraft, setKeepCountDraft] = useState<string | null>(null);

  if (isLoading) return null;

  const name = nameDraft ?? deviceName;
  const keepCount = keepCountDraft ?? String(settings.keepCount);

  const saveName = async () => {
    const trimmed = name.trim();
    if (trimmed === deviceName) {
      setNameDraft(null);
      return;
    }
    try {
      await Backup.RenameDevice(trimmed);
      await queryClient.invalidateQueries({ queryKey: backupOverviewKey });
    } catch (error) {
      toast.error(backupErrorMessage(t, error));
    }
    setNameDraft(null);
  };

  const saveKeepCount = () => {
    setKeepCountDraft(null);
    const value = Number.parseInt(keepCount, 10);
    if (!Number.isFinite(value)) return;
    const clamped = Math.min(30, Math.max(1, value));
    if (clamped !== settings.keepCount) void update("keepCount", clamped);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">{t("page.backup.schedule.title")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <Row
          title={t("page.backup.schedule.enabled.title")}
          description={t("page.backup.schedule.enabled.description")}
        >
          <Switch
            checked={settings.enabled}
            onCheckedChange={(value) => update("enabled", value)}
          />
        </Row>
        <Separator />
        <Row
          title={t("page.backup.schedule.interval.title")}
          description={t("page.backup.schedule.interval.description")}
        >
          <Select
            value={settings.interval}
            items={INTERVALS.map((value) => ({
              value,
              label: t(`page.backup.schedule.interval.options.${value}`),
            }))}
            onValueChange={(value) => update("interval", value as BackupInterval)}
          >
            <SelectTrigger className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {INTERVALS.map((value) => (
                  <SelectItem key={value} value={value}>
                    {t(`page.backup.schedule.interval.options.${value}`)}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Row>
        <Separator />
        <Row
          title={t("page.backup.schedule.on_startup.title")}
          description={t("page.backup.schedule.on_startup.description")}
        >
          <Switch
            checked={settings.onStartup}
            onCheckedChange={(value) => update("onStartup", value)}
          />
        </Row>
        <Separator />
        <Row
          title={t("page.backup.schedule.watch_changes.title")}
          description={t("page.backup.schedule.watch_changes.description")}
        >
          <Switch
            checked={settings.watchChanges}
            onCheckedChange={(value) => update("watchChanges", value)}
          />
        </Row>
        <Separator />
        <Row
          title={t("page.backup.schedule.keep_count.title")}
          description={t("page.backup.schedule.keep_count.description")}
        >
          <Input
            type="number"
            min={1}
            max={30}
            className="w-24 tabular-nums"
            value={keepCount}
            onChange={(event) => setKeepCountDraft(event.target.value)}
            onBlur={saveKeepCount}
            onKeyDown={(event) => {
              if (event.key === "Enter") event.currentTarget.blur();
            }}
          />
        </Row>
        <Separator />
        <Row
          title={t("page.backup.schedule.device_name.title")}
          description={t("page.backup.schedule.device_name.description")}
        >
          <Input
            className="w-56"
            maxLength={80}
            value={name}
            onChange={(event) => setNameDraft(event.target.value)}
            onBlur={() => void saveName()}
            onKeyDown={(event) => {
              if (event.key === "Enter") event.currentTarget.blur();
            }}
          />
        </Row>
      </CardContent>
    </Card>
  );
}
