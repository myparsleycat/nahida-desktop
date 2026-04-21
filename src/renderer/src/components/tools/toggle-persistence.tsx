import { Switch } from "@renderer/components/ui/switch";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ScrollArea } from "../ui/scroll-area";

export default function TogglePersistence() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const { data: xxmiData } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => window.api.invoke("xxmi:getXXMIData"),
  });

  const { data: enabled, isPending: isQueryPending } = useQuery({
    queryKey: ["setting:xxmi:getPersistToggles"],
    queryFn: () => window.api.invoke("setting:xxmi:getPersistToggles"),
  });

  const { data: logs = [] } = useQuery<string[]>({
    queryKey: ["setting:xxmi:getPersistLogs"],
    queryFn: () => window.api.invoke("setting:xxmi:getPersistLogs"),
  });

  useEffect(() => {
    const unsubscribe = window.api.on("setting:xxmi:persistLogs", (nextLogs) => {
      queryClient.setQueryData(["setting:xxmi:getPersistLogs"], nextLogs);
    });

    return unsubscribe;
  }, [queryClient]);

  const { mutate, isPending: isMutatePending } = useMutation({
    mutationFn: (newEnabled: boolean) =>
      window.api.invoke("setting:xxmi:setPersistToggles", newEnabled),
    onSuccess: (_, newEnabled) => {
      queryClient.setQueryData(["setting:xxmi:getPersistToggles"], newEnabled);
    },
    onError: (err) => {
      toast.error(err.message);
    },
  });

  if (!xxmiData?.xxmiPath) {
    return (
      <div className="flex flex-col items-center justify-center w-full p-2 text-center">
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
      <div className="flex flex-row items-center justify-between w-full p-3 rounded-lg border hover:shadow transition-shadow duration-200">
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

      <div className="flex flex-col w-full p-3 rounded-lg border hover:shadow transition-shadow duration-200 h-80">
        <div className="text-sm font-medium text-muted-foreground mb-2">Logs</div>
        <ScrollArea className="flex-1 overflow-auto rounded border bg-muted/30 p-2">
          {logs.length === 0 ? (
            <div className="text-sm text-muted-foreground italic">No logs yet.</div>
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
