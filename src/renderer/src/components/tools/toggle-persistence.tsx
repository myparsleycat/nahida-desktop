import { Switch } from "@renderer/components/ui/switch";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function TogglePersistence() {
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

  const { mutate, isPending: isMutatePending } = useMutation({
    mutationFn: (newEnabled: boolean) =>
      window.api.invoke("setting:xxmi:setPersistToggles", newEnabled),
    onSuccess: (_, newEnabled) => {
      queryClient.setQueryData(["setting:xxmi:getPersistToggles"], newEnabled);
    },
    onError: (err: any) => {
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
          XXMI Launcher를 먼저 설정해 주세요.
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-row items-center justify-between w-full p-2">
      <div className="flex flex-col space-y-1">
        <h3 className="text-lg font-semibold">{t("page.setting.xxmi.persistToggles")}</h3>
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
  );
}
