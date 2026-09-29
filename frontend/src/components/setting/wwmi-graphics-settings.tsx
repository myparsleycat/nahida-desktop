import type { WWMIOptions } from "@bindings/xxmi/models";
import { Input } from "@renderer/components/ui/input";
import { Switch } from "@renderer/components/ui/switch";
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
        <label key={field} className="block space-y-1">
          <span>{t(`page.setting.xxmi.builtin.${field}`)}</span>
          <Input
            type="number"
            step={
              field === "meshLODDistanceBaseFOV" || field === "textureStreamingPoolSize" ? 1 : "any"
            }
            value={options[field]}
            onChange={(event) => onChange({ ...options, [field]: Number(event.target.value) })}
          />
        </label>
      ))}
      {(
        [
          "textureStreamingUseAllMips",
          "textureStreamingLimitToVRAM",
          "textureStreamingFixedPoolSize",
        ] as const
      ).map((field) => (
        <label key={field} className="flex items-center justify-between">
          <span>{t(`page.setting.xxmi.builtin.${field}`)}</span>
          <Switch
            checked={options[field]}
            onCheckedChange={(value) => onChange({ ...options, [field]: value })}
          />
        </label>
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
