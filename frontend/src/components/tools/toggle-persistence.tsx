import { Setting } from "@bindings/setting";
import { Tools } from "@bindings/tools";
import { XXMI } from "@bindings/xxmi";
import {
  Section,
  SectionContent,
  SectionHeader,
  SectionTitle,
} from "@renderer/components/ui/section";
import { Switch } from "@renderer/components/ui/switch";
import { cn } from "@renderer/lib/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import { ArrowRightIcon, FolderSearchIcon } from "lucide-react";
import { useEffect, useId } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Button } from "../ui/button";

// Backend entries look like "[Jan 2, 2006, 3:04:05 PM] [INFO] message".
const logPattern = /^\[(.+?)\] \[(INFO|ERROR)\] ([\s\S]*)$/;

export default function TogglePersistence() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const titleId = useId();

  const { data: xxmiData, isPending: isXXMIPending } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => XXMI.GetXXMIData(),
  });

  const { data: enabled, isPending: isQueryPending } = useQuery({
    queryKey: ["setting:xxmi:getPersistToggles"],
    queryFn: () => Setting.GetPersistToggles(),
  });

  const { data: logs = [] } = useQuery<string[]>({
    queryKey: ["setting:xxmi:getPersistLogs"],
    queryFn: async () => (await Tools.GetPersistLogs()) ?? [],
  });

  useEffect(() => {
    const off = Events.On("setting:xxmi:persistLogs", (event) => {
      queryClient.setQueryData(["setting:xxmi:getPersistLogs"], event.data);
    });

    return off;
  }, [queryClient]);

  const { mutate, isPending: isMutatePending } = useMutation({
    mutationFn: (newEnabled: boolean) => Setting.SetPersistToggles(newEnabled),
    onSuccess: (_, newEnabled) => {
      queryClient.setQueryData(["setting:xxmi:getPersistToggles"], newEnabled);
    },
    onError: (err) => {
      toast.error(err.message);
    },
  });

  if (isXXMIPending) return null;

  if (!xxmiData?.xxmiPath) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-4 text-center">
        <FolderSearchIcon className="size-8 text-muted-foreground" />
        <div className="space-y-1">
          <h3 className="text-sm font-medium text-foreground">
            {t("page.setting.xxmi.persistToggles")}
          </h3>
          <p className="text-sm text-muted-foreground">
            {t("page.setting.xxmi.persistNotFoundXXMI")}
          </p>
        </div>
        <Button render={<Link to="/xxmi" />} variant="outline" size="sm">
          {t("page.setting.xxmi.xxmiPath")}
          <ArrowRightIcon />
        </Button>
      </div>
    );
  }

  return (
    <div className="h-full space-y-6 overflow-y-auto p-4">
      <div className="flex items-start justify-between gap-6">
        <div className="min-w-0 space-y-1">
          <h2 id={titleId} className="text-lg font-semibold text-foreground">
            {t("page.setting.xxmi.persistToggles")}
          </h2>
          <p className="text-sm text-muted-foreground">
            {t("page.setting.xxmi.persistTogglesDescription")}
          </p>
        </div>
        <Switch
          aria-labelledby={titleId}
          className="mt-1"
          checked={!!enabled}
          onCheckedChange={(c) => mutate(c)}
          disabled={isQueryPending || isMutatePending}
        />
      </div>

      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.setting.xxmi.persistLogs")}</SectionTitle>
        </SectionHeader>
        <SectionContent layout="flow">
          {logs.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              {t("page.setting.xxmi.persistLogsEmpty")}
            </p>
          ) : (
            <ol className="space-y-1.5 rounded-md bg-muted/50 p-2 font-mono text-xs">
              {[...logs].reverse().map((log, index) => {
                const match = logPattern.exec(log);
                if (!match) {
                  return (
                    <li key={`${log}-${index}`} className="break-all">
                      {log}
                    </li>
                  );
                }

                const isError = match[2] === "ERROR";
                return (
                  <li key={`${log}-${index}`} className="flex flex-col gap-x-2 sm:flex-row">
                    <span className="shrink-0 text-muted-foreground">{match[1]}</span>
                    <span
                      className={cn(
                        "w-10 shrink-0 font-medium",
                        isError ? "text-destructive" : "text-muted-foreground",
                      )}
                    >
                      {match[2]}
                    </span>
                    <span
                      className={cn(
                        "min-w-0 break-all whitespace-pre-wrap",
                        isError && "text-destructive",
                      )}
                    >
                      {match[3]}
                    </span>
                  </li>
                );
              })}
            </ol>
          )}
        </SectionContent>
      </Section>
    </div>
  );
}
