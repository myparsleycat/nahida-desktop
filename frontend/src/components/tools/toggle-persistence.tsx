import { Setting } from "@bindings/setting";
import { Tools } from "@bindings/tools";
import { XXMI } from "@bindings/xxmi";
import { Switch } from "@renderer/components/ui/switch";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { ScrollArea } from "../ui/scroll-area";

export default function TogglePersistence() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const { data: xxmiData } = useQuery({
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

  if (!xxmiData?.xxmiPath) {
    return (
      <div className="flex w-full flex-col items-center justify-center p-2 text-center">
        <h3 className="text-lg font-semibold text-muted-foreground">
          {t("page.setting.xxmi.persistToggles")}
        </h3>
        <p className="text-sm text-muted-foreground italic">
          {t("page.setting.xxmi.persistNotFoundXXMI")}
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col space-y-4 p-4">
      <div className="flex w-full flex-row items-center justify-between rounded-lg border p-3 transition-shadow duration-200 hover:shadow">
        <div className="flex flex-col space-y-1">
          <h3 className="text font-semibold">{t("page.setting.xxmi.persistToggles")}</h3>
          <p className="text-sm text-muted-foreground">
            {t("page.setting.xxmi.persistTogglesDescription")}
          </p>
        </div>
        <Switch
          checked={!!enabled}
          onCheckedChange={(c) => mutate(c)}
          disabled={isQueryPending || isMutatePending}
        />
      </div>

      <div className="flex h-80 w-full flex-col rounded-lg border p-3 transition-shadow duration-200 hover:shadow">
        <div className="mb-2 text-sm font-medium text-muted-foreground">Logs</div>
        <ScrollArea className="flex-1 overflow-auto rounded border bg-muted/30 p-2">
          {logs.length === 0 ? (
            <div className="text-sm text-muted-foreground italic">No logs yet.</div>
          ) : (
            <div className="flex flex-col gap-1">
              {[...logs].reverse().map((log, index) => (
                <div key={`${log}-${index}`} className="font-mono text-xs break-all">
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
