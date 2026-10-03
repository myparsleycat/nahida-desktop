import type { WWMIOptions } from "@bindings/xxmi/models";
import { NumberRow, SelectRow } from "@renderer/components/xxmi/xxmi-fields";
import { useTranslation } from "react-i18next";

type Props = {
  options: WWMIOptions;
  onChange: (options: WWMIOptions) => void;
};

export function WWMIGraphicsSettings({ options, onChange }: Props) {
  const { t } = useTranslation();

  return (
    <>
      <SelectRow
        label={t("page.setting.xxmi.builtin.resourceTier")}
        description={t("page.setting.xxmi.builtin.resourceTierDescription")}
        value={options.resourceTier}
        options={["UHD", "HD", "SD"]}
        // Picking a tier here answers the question the first launch would otherwise ask.
        onValueChange={(resourceTier) =>
          onChange({ ...options, resourceTier, resourceTierDecided: true })
        }
      />
      <NumberRow
        label={t("page.setting.xxmi.builtin.meshLODDistanceBaseFOV")}
        description={t("page.setting.xxmi.builtin.meshLODDistanceBaseFOVDescription")}
        step={1}
        value={options.meshLODDistanceBaseFOV}
        onValueChange={(meshLODDistanceBaseFOV) => onChange({ ...options, meshLODDistanceBaseFOV })}
      />
    </>
  );
}
