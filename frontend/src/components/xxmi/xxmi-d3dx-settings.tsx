import { XXMI } from "@bindings/xxmi";
import {
  D3DXOptionKind,
  D3DXOptionManaged,
  type D3DXOption,
  type ImporterConfig,
} from "@bindings/xxmi/models";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  Section,
  SectionContent,
  SectionDescription,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import { NumberRow, SelectRow, ToggleRow } from "@renderer/components/xxmi/xxmi-fields";
import { useQuery } from "@tanstack/react-query";
import { groupBy, omit } from "es-toolkit";
import { useTranslation } from "react-i18next";

export function XXMID3DXSettings({
  importer,
  config,
  savedOverrides,
  onChange,
}: {
  importer: string;
  config: ImporterConfig;
  savedOverrides: ImporterConfig["d3dxOverrides"];
  onChange: (next: ImporterConfig) => void;
}) {
  const { t } = useTranslation();
  const { data: options } = useQuery({
    queryKey: ["xxmi:d3dx-options", importer],
    queryFn: () => XXMI.GetD3DXOptions(importer),
  });

  if (!options) return null;
  if (options.length === 0) {
    return (
      <p className="px-1 text-sm text-muted-foreground">
        {t("page.setting.xxmi.builtin.d3dx.empty")}
      </p>
    );
  }

  const setOverrides = (overrides: Record<string, string>) =>
    // The saved config has no field without overrides, so an empty one would read as an unsaved change.
    onChange(
      Object.keys(overrides).length === 0
        ? omit(config, ["d3dxOverrides"])
        : { ...config, d3dxOverrides: overrides },
    );
  const setValue = (option: D3DXOption, value: string) => {
    const id = `${option.section}.${option.key}`;
    // A saved override stays even when the file already holds its value: the launch that wrote it there is
    // also what restores it after a package update replaces d3dx.ini. Only clearing it removes it.
    const untouched = value === option.value && savedOverrides?.[id] === undefined;
    setOverrides(
      untouched ? omit(config.d3dxOverrides ?? {}, [id]) : { ...config.d3dxOverrides, [id]: value },
    );
  };

  return (
    <>
      <p className="px-1 text-xs text-muted-foreground">
        {t("page.setting.xxmi.builtin.d3dx.applyNotice")}
      </p>
      {Object.entries(groupBy(options, (option) => option.section)).map(([section, entries]) => (
        <Section key={section}>
          <SectionHeader>
            <SectionTitle className="font-mono">[{section}]</SectionTitle>
            <SectionDescription>
              {t(`page.setting.xxmi.builtin.d3dx.sections.${section}`)}
            </SectionDescription>
          </SectionHeader>
          <SectionContent>
            {entries.map((option) => {
              const id = `${option.section}.${option.key}`;
              const override = config.d3dxOverrides?.[id];
              return (
                <OptionRow
                  key={option.key}
                  option={option}
                  value={override ?? option.value}
                  locked={
                    option.managed === D3DXOptionManaged.D3DXOptionManagedLaunch ||
                    (option.managed === D3DXOptionManaged.D3DXOptionManagedRendering &&
                      config.migoto.enforceRendering)
                  }
                  onValueChange={(value) => setValue(option, value)}
                  onClear={
                    override === undefined
                      ? undefined
                      : () => setOverrides(omit(config.d3dxOverrides ?? {}, [id]))
                  }
                />
              );
            })}
          </SectionContent>
        </Section>
      ))}
    </>
  );
}

function OptionRow({
  option,
  value,
  locked,
  onValueChange,
  onClear,
}: {
  option: D3DXOption;
  value: string;
  locked: boolean;
  onValueChange: (value: string) => void;
  onClear?: () => void;
}) {
  const { t } = useTranslation();
  const label = <span className="font-mono">{option.key}</span>;
  const description = (
    <>
      {t(`page.setting.xxmi.builtin.d3dx.options.${option.section}.${option.key}`)}
      {locked && (
        <span className="mt-0.5 block text-foreground/70">
          {t(
            option.managed === D3DXOptionManaged.D3DXOptionManagedRendering
              ? "page.setting.xxmi.builtin.d3dx.managedRenderingHint"
              : "page.setting.xxmi.builtin.d3dx.managedHint",
          )}
        </span>
      )}
      {!locked && onClear && (
        <span className="mt-0.5 flex items-center gap-1 text-foreground/70">
          {t("page.setting.xxmi.builtin.d3dx.overrideHint")}
          <Button variant="link" size="xs" className="h-auto px-0" onClick={onClear}>
            {t("page.setting.xxmi.builtin.d3dx.clearOverride")}
          </Button>
        </span>
      )}
    </>
  );

  switch (option.kind) {
    case D3DXOptionKind.D3DXOptionBool:
      return (
        <ToggleRow
          label={label}
          description={description}
          checked={["1", "true", "yes", "on"].includes(value.toLowerCase())}
          disabled={locked}
          onCheckedChange={(checked) => onValueChange(checked ? "1" : "0")}
        />
      );
    case D3DXOptionKind.D3DXOptionEnum:
      return (
        <SelectRow
          label={label}
          description={description}
          value={value}
          disabled={locked}
          // A value the file holds outside the known choices stays selectable instead of showing a blank control.
          options={[...new Set([...(option.choices ?? []), value])].map((choice) => ({
            value: choice,
            label: t(
              `page.setting.xxmi.builtin.d3dx.choices.${option.section}.${option.key}.${choice}`,
              { defaultValue: choice },
            ),
          }))}
          onValueChange={onValueChange}
        />
      );
    case D3DXOptionKind.D3DXOptionInt:
    case D3DXOptionKind.D3DXOptionFloat:
      return (
        <NumberRow
          label={label}
          description={description}
          value={Number(value)}
          min={option.min ?? undefined}
          max={option.max ?? undefined}
          step={option.kind === D3DXOptionKind.D3DXOptionFloat ? "any" : 1}
          disabled={locked}
          onValueChange={(next) => onValueChange(String(next))}
        />
      );
    default:
      return (
        <div className="space-y-1.5">
          <SectionRow title={label} description={description} />
          <Input
            aria-label={option.key}
            className="font-mono"
            spellCheck={false}
            disabled={locked}
            value={value}
            onChange={(event) => onValueChange(event.target.value)}
          />
        </div>
      );
  }
}
