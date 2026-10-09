import {
  Section,
  SectionContent,
  SectionHeader,
  SectionTitle,
} from "@renderer/components/ui/section";
import { ToggleRow } from "@renderer/components/xxmi/xxmi-fields";
import { useSettings } from "@renderer/hooks/use-settings";
import { useTranslation } from "react-i18next";

const settingsConfig = {
  enabled: "xxmi.launchGuard",
  dcr: "xxmi.launchGuardDcr",
  smoothMotion: "xxmi.launchGuardSmoothMotion",
  logging: "xxmi.launchGuardLogging",
  textures: "xxmi.launchGuardTextures",
} as const;
const guards = ["dcr", "smoothMotion", "logging", "textures"] as const;

export function XXMILaunchGuardSettings() {
  const { t } = useTranslation();
  const { settings, update } = useSettings(settingsConfig);

  return (
    <Section>
      <SectionHeader>
        <SectionTitle>{t("page.setting.xxmi.launchGuard.title")}</SectionTitle>
      </SectionHeader>
      <SectionContent>
        <ToggleRow
          label={t("page.setting.xxmi.launchGuard.enabled")}
          description={t("page.setting.xxmi.launchGuard.enabledDescription")}
          checked={settings.enabled ?? true}
          onCheckedChange={(value) => void update("enabled", value)}
        >
          {guards.map((guard) => (
            <ToggleRow
              key={guard}
              label={t(`page.setting.xxmi.launchGuard.${guard}`)}
              description={t(`page.setting.xxmi.launchGuard.${guard}Description`)}
              checked={settings[guard] ?? guard !== "textures"}
              onCheckedChange={(value) => void update(guard, value)}
            />
          ))}
        </ToggleRow>
      </SectionContent>
    </Section>
  );
}
