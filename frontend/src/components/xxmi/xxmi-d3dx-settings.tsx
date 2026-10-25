import { XXMI } from "@bindings/xxmi";
import { D3DXOptionManaged, type D3DXOption, type ImporterConfig } from "@bindings/xxmi/models";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  Section,
  SectionContent,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { toErrorMessage } from "@shared/utils";
import { useQuery } from "@tanstack/react-query";
import { groupBy, omit } from "es-toolkit";
import { SearchIcon } from "lucide-react";
import { useId, useRef, useState, type FocusEvent } from "react";
import { useTranslation } from "react-i18next";

type StatusFilter = "all" | "overridden" | "editable" | "managed";

// Base UI selects cannot represent an empty string as a regular option value.
const ALL_SECTIONS = "__all__";

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
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [chosenSection, setChosenSection] = useState(ALL_SECTIONS);
  const [focusedRow, setFocusedRow] = useState<string | null>(null);
  const query = useQuery({
    queryKey: ["xxmi:d3dx-options", importer],
    queryFn: () => XXMI.GetD3DXOptions(importer),
  });
  const options = query.data;

  if (!options) {
    if (!query.isError) return null;
    return (
      <div className="flex items-center justify-between gap-4 px-1">
        <div className="min-w-0 space-y-0.5">
          <p className="text-sm text-destructive">
            {t("page.setting.xxmi.builtin.d3dx.loadFailed")}
          </p>
          <p className="text-xs break-words text-muted-foreground">{toErrorMessage(query.error)}</p>
        </div>
        <Button variant="outline" size="sm" onClickPromise={() => query.refetch()}>
          {t("page.setting.xxmi.dllVersionRetry")}
        </Button>
      </div>
    );
  }
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
    const id = overrideId(config.d3dxOverrides, option);
    // A saved override stays even when the file already holds its value: the launch that wrote it there is
    // also what restores it after a package update replaces d3dx.ini. Only clearing it removes it.
    const untouched =
      value === option.value && savedOverrides?.[overrideId(savedOverrides, option)] === undefined;
    setOverrides(
      untouched ? omit(config.d3dxOverrides ?? {}, [id]) : { ...config.d3dxOverrides, [id]: value },
    );
  };

  const isLocked = (option: D3DXOption) =>
    option.managed === D3DXOptionManaged.D3DXOptionManagedLaunch ||
    (option.managed === D3DXOptionManaged.D3DXOptionManagedRendering &&
      config.migoto.enforceRendering);

  const sections = Object.entries(groupBy(options, (option) => option.section.toLowerCase()));
  // A section a refetch no longer lists falls back to showing every section.
  const section = sections.some(([name]) => name === chosenSection) ? chosenSection : ALL_SECTIONS;
  const needle = search.trim().toLowerCase();
  const matches = (option: D3DXOption) => {
    // The search and status filters read the value being typed, so the row being edited stays until focus leaves
    // it instead of unmounting under the cursor.
    if (focusedRow === rowId(option)) return true;
    const override = config.d3dxOverrides?.[overrideId(config.d3dxOverrides, option)];
    const matchesStatus =
      status === "all" ||
      (status === "overridden" && override !== undefined) ||
      (status === "editable" && !isLocked(option)) ||
      (status === "managed" && isLocked(option));
    return (
      matchesStatus &&
      `${option.section}.${option.key}\n${override ?? option.value}`.toLowerCase().includes(needle)
    );
  };
  const shown = sections
    .filter(([name]) => section === ALL_SECTIONS || name === section)
    .map(([name, entries]) => [name, entries.filter(matches)] as const)
    .filter(([, entries]) => entries.length > 0);

  return (
    <>
      <p className="px-1 text-xs text-muted-foreground">
        {t("page.setting.xxmi.builtin.d3dx.applyNotice")}
      </p>
      <div className="flex gap-2">
        <div className="relative flex flex-1 items-center">
          <SearchIcon className="absolute left-2 size-4 text-muted-foreground" />
          <Input
            className="h-8 pl-7 dark:bg-transparent"
            placeholder={t("g.search")}
            aria-label={t("g.search")}
            spellCheck={false}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </div>
        <FilterSelect
          label={t("page.setting.xxmi.builtin.d3dx.statusFilter")}
          value={status}
          items={[
            { value: "all", label: t("page.setting.xxmi.builtin.d3dx.statusAll") },
            { value: "overridden", label: t("page.setting.xxmi.builtin.d3dx.statusOverridden") },
            { value: "editable", label: t("page.setting.xxmi.builtin.d3dx.statusEditable") },
            { value: "managed", label: t("page.setting.xxmi.builtin.d3dx.statusManaged") },
          ]}
          onValueChange={setStatus}
        />
        <FilterSelect
          label={t("page.setting.xxmi.builtin.d3dx.sectionFilter")}
          value={section}
          items={[
            { value: ALL_SECTIONS, label: t("page.setting.xxmi.builtin.d3dx.sectionAll") },
            ...sections.map(([name, entries]) => ({
              value: name,
              label: `[${entries[0].section}]`,
            })),
          ]}
          onValueChange={setChosenSection}
        />
      </div>
      {shown.length === 0 && (
        <p className="px-1 text-xs text-muted-foreground">
          {t("page.setting.xxmi.builtin.d3dx.noMatch")}
        </p>
      )}
      {shown.map(([name, entries]) => (
        <Section key={name}>
          <SectionHeader>
            <SectionTitle className="font-mono">[{entries[0].section}]</SectionTitle>
          </SectionHeader>
          <SectionContent>
            {entries.map((option) => {
              const id = overrideId(config.d3dxOverrides, option);
              const override = config.d3dxOverrides?.[id];
              return (
                <OptionRow
                  key={option.key}
                  option={option}
                  value={override ?? option.value}
                  locked={isLocked(option)}
                  onFocus={() => setFocusedRow(rowId(option))}
                  onBlur={(event) => {
                    if (event.currentTarget.contains(event.relatedTarget)) return;
                    setFocusedRow((current) => (current === rowId(option) ? null : current));
                  }}
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

function FilterSelect<T extends string>({
  label,
  value,
  items,
  onValueChange,
}: {
  label: string;
  value: T;
  items: { value: T; label: string }[];
  onValueChange: (value: T) => void;
}) {
  return (
    <Select
      value={value}
      items={items}
      onValueChange={(next) => {
        if (next !== null) onValueChange(next);
      }}
    >
      <SelectTrigger className="w-fit max-w-60 min-w-36 shrink-0" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

function rowId(option: D3DXOption) {
  return `${option.section}.${option.key}`.toLowerCase();
}

// d3dx.ini names are case-insensitive, and a saved override can spell the option differently than the file does.
function overrideId(overrides: ImporterConfig["d3dxOverrides"], option: D3DXOption) {
  const id = `${option.section}.${option.key}`;
  return Object.keys(overrides ?? {}).find((key) => key.toLowerCase() === id.toLowerCase()) ?? id;
}

function OptionRow({
  option,
  value,
  locked,
  onFocus,
  onBlur,
  onValueChange,
  onClear,
}: {
  option: D3DXOption;
  value: string;
  locked: boolean;
  onFocus: () => void;
  onBlur: (event: FocusEvent<HTMLDivElement>) => void;
  onValueChange: (value: string) => void;
  onClear?: () => void;
}) {
  const { t } = useTranslation();
  const labelId = useId();
  const inputRef = useRef<HTMLInputElement>(null);

  return (
    <SectionRow
      title={<span className="font-mono">{option.key}</span>}
      titleId={labelId}
      onFocus={onFocus}
      onBlur={onBlur}
      description={
        locked ? (
          <span className="text-foreground/70">
            {t(
              option.managed === D3DXOptionManaged.D3DXOptionManagedRendering
                ? "page.setting.xxmi.builtin.d3dx.managedRenderingHint"
                : "page.setting.xxmi.builtin.d3dx.managedHint",
            )}
          </span>
        ) : (
          onClear && (
            <span className="flex items-center gap-1 text-foreground/70">
              {t("page.setting.xxmi.builtin.d3dx.overrideHint")}
              <Button
                variant="link"
                size="xs"
                className="h-auto px-0"
                onClick={() => {
                  // Clearing removes this button, and a removed element fires no blur, so the row would keep
                  // bypassing the filters. Focus moves to the input first, which still blurs later.
                  inputRef.current?.focus();
                  onClear();
                }}
              >
                {t("page.setting.xxmi.builtin.d3dx.clearOverride")}
              </Button>
            </span>
          )
        )
      }
    >
      <Input
        ref={inputRef}
        aria-labelledby={labelId}
        className="w-72 shrink-0 font-mono"
        spellCheck={false}
        disabled={locked}
        value={value}
        onChange={(event) => onValueChange(event.target.value)}
      />
    </SectionRow>
  );
}
