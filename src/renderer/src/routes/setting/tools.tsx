import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Switch } from "@renderer/components/ui/switch";
import { useSettings } from "@renderer/hooks/use-settings";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/tools")({
  component: RouteComponent,
});

const settingsConfig = {
  wuwaFixerUpdateNotification: "tools.wuwaFixerUpdateNotification",
} as const;

function RouteComponent() {
  const { t } = useTranslation();
  const { settings, update, isLoading } = useSettings(settingsConfig);

  if (isLoading) {
    return null;
  }

  return (
    <div className="space-y-6 p-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            {t("page.setting.tools.wuwaFixer.title")}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between gap-6">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.tools.wuwaFixer.autoUpdateNotification.title")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.tools.wuwaFixer.autoUpdateNotification.description")}
              </p>
            </div>
            <Switch
              checked={settings.wuwaFixerUpdateNotification}
              onCheckedChange={(val) => update("wuwaFixerUpdateNotification", val)}
            />
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
