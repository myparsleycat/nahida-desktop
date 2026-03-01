import { Card, CardDescription, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Switch } from "@renderer/components/ui/switch";
import type { XXMIData } from "@renderer/routes/setting/xxmi";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function XXMIPersist({ xxmiData }: { xxmiData?: XXMIData }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

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
    onError: (err) => {
      toast.error(err.message);
    },
  });

  if (!xxmiData?.xxmiPath) {
    return null;
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div className="space-y-1">
          <CardTitle className="text-base text-md font-medium">
            {t("page.setting.xxmi.persistToggles")}
          </CardTitle>
          <CardDescription>{t("page.setting.xxmi.persistTogglesDescription")}</CardDescription>
        </div>
        <Switch
          checked={!!enabled}
          onCheckedChange={(c) => mutate(c)}
          disabled={isQueryPending || isMutatePending}
        />
      </CardHeader>
    </Card>
  );
}
