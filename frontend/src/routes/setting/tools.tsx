import {
  Section,
  SectionContent,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import { Switch } from "@renderer/components/ui/switch";
import { useSettings } from "@renderer/hooks/use-settings";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/tools")({
  component: RouteComponent,
});

const settingsConfig = {
  wuwaFixerUpdateNotification: "tools.wuwaFixerUpdateNotification",
} as const;

const WUWA_FIXER_AUTO_UPDATE_NOTIFICATION_LABEL_ID =
  "setting-tools-wuwa-fixer-auto-update-notification-title";

function RouteComponent() {
  const { t } = useTranslation();
  const { settings, update, isLoading } = useSettings(settingsConfig);

  if (isLoading) {
    return null;
  }

  const handleWuwaFixerUpdateNotificationChange = async (val: boolean) => {
    const previous = settings.wuwaFixerUpdateNotification;
    try {
      await update("wuwaFixerUpdateNotification", val);
    } catch {
      await update("wuwaFixerUpdateNotification", previous).catch(() => {});
      toast.error(t("page.setting.tools.wuwaFixer.autoUpdateNotification.save_failed"));
    }
  };

  return (
    <div className="space-y-6 p-4">
      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.setting.tools.wuwaFixer.title")}</SectionTitle>
        </SectionHeader>
        <SectionContent>
          <SectionRow
            titleId={WUWA_FIXER_AUTO_UPDATE_NOTIFICATION_LABEL_ID}
            title={t("page.setting.tools.wuwaFixer.autoUpdateNotification.title")}
            description={t("page.setting.tools.wuwaFixer.autoUpdateNotification.description")}
          >
            <Switch
              checked={settings.wuwaFixerUpdateNotification}
              aria-labelledby={WUWA_FIXER_AUTO_UPDATE_NOTIFICATION_LABEL_ID}
              onCheckedChange={(val) => void handleWuwaFixerUpdateNotificationChange(val)}
            />
          </SectionRow>
        </SectionContent>
      </Section>
    </div>
  );
}
