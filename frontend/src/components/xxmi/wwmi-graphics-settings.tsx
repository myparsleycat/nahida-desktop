import type { WWMIOptions } from "@bindings/xxmi/models";
import { Input } from "@renderer/components/ui/input";
import { NumberRow, ToggleRow } from "@renderer/components/xxmi/xxmi-fields";
import { useTranslation } from "react-i18next";

type Props = {
  options: WWMIOptions;
  onChange: (options: WWMIOptions) => void;
};

export function WWMIGraphicsSettings({ options, onChange }: Props) {
  const { t } = useTranslation();

  return (
    <>
      {(
        [
          "meshLODDistanceBaseFOV",
          "meshLODDistanceScale",
          "meshLODDistanceOffset",
          "textureStreamingBoost",
          "textureStreamingMinBoost",
          "textureStreamingPoolSize",
        ] as const
      ).map((field) => (
        <NumberRow
          key={field}
          label={t(`page.setting.xxmi.builtin.${field}`)}
          description={
            field === "meshLODDistanceScale"
              ? undefined
              : t(`page.setting.xxmi.builtin.${field}Description`)
          }
          step={
            field === "meshLODDistanceBaseFOV" || field === "textureStreamingPoolSize" ? 1 : "any"
          }
          value={options[field]}
          onValueChange={(value) => onChange({ ...options, [field]: value })}
        />
      ))}
      {(
        [
          "textureStreamingUseAllMips",
          "textureStreamingLimitToVRAM",
          "textureStreamingFixedPoolSize",
        ] as const
      ).map((field) => (
        <ToggleRow
          key={field}
          label={t(`page.setting.xxmi.builtin.${field}`)}
          description={t(`page.setting.xxmi.builtin.${field}Description`)}
          checked={options[field]}
          onCheckedChange={(value) => onChange({ ...options, [field]: value })}
        />
      ))}
      {options.applyPerfTweaks && (
        <div className="space-y-2">
          <p>{t("page.setting.xxmi.builtin.perfTweakValues")}</p>
          {Object.entries(options.perfTweaks ?? {}).map(([name, value]) => (
            <label key={name} className="block space-y-1">
              <span className="break-all">{name}</span>
              <Input
                type="number"
                step="any"
                value={value ?? ""}
                onChange={(event) =>
                  onChange({
                    ...options,
                    perfTweaks: { ...options.perfTweaks, [name]: Number(event.target.value) },
                  })
                }
              />
            </label>
          ))}
        </div>
      )}
    </>
  );
}
