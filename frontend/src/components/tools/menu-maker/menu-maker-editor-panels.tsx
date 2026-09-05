import type { MenuMakerScanResult } from "@bindings/menumaker";
import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Field, FieldLabel, FieldLegend, FieldSet } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { cn } from "@renderer/lib/utils";
import { updateSlotKey } from "@shared/menu-maker/parser";
import {
  MENU_MAKER_BASE_SLOT_SIZE,
  type MenuMakerSettings,
  type MenuMakerSlot,
} from "@shared/menu-maker/types";
import { ImageIcon, UploadIcon } from "lucide-react";
import { DynamicIcon, iconNames } from "lucide-react/dynamic";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { type EditorAction } from "./menu-maker-editor-state";

export function Inspector({
  settings,
  dispatch,
}: {
  settings: MenuMakerSettings;
  dispatch: React.Dispatch<EditorAction>;
}) {
  const { t } = useTranslation();
  const set = <K extends keyof MenuMakerSettings>(key: K, value: MenuMakerSettings[K]) =>
    dispatch({ type: "settings", value: { [key]: value } });
  return (
    <div className="space-y-5">
      <FieldSet className="gap-2">
        <FieldLegend className="mb-2 text-xs font-semibold text-muted-foreground uppercase">
          {t("page.tools.menu_maker.panel")}
        </FieldLegend>
        <SettingField label={t("page.tools.menu_maker.menu_title")}>
          <Input
            value={settings.title}
            placeholder={t("page.tools.menu_maker.menu_title_placeholder")}
            onChange={(event) => set("title", event.target.value)}
          />
        </SettingField>
        <div className="grid grid-cols-2 gap-2">
          <SettingField label={t("page.tools.menu_maker.menu_key")}>
            <Input
              value={settings.menuKey}
              onChange={(event) => set("menuKey", event.target.value)}
            />
          </SettingField>
          <SettingField label={t("page.tools.menu_maker.click_modifier")}>
            <SettingSelect
              value={settings.clickModifier}
              items={CLICK_MODIFIERS}
              onChange={(value) => set("clickModifier", value)}
              className="w-full"
            />
          </SettingField>
        </div>
        <div className="grid grid-cols-3 gap-2">
          <NumberField
            label={t("page.tools.menu_maker.columns")}
            value={settings.columns}
            min={1}
            max={8}
            onChange={(value) => set("columns", value)}
          />
          <NumberField
            label={t("page.tools.menu_maker.gap")}
            value={settings.gap}
            min={0}
            max={64}
            onChange={(value) => set("gap", value)}
          />
          <NumberField
            label={t("page.tools.menu_maker.scale")}
            value={settings.panelScale}
            min={0.5}
            max={2}
            step={0.1}
            onChange={(value) => set("panelScale", value)}
          />
        </div>
        <div className="grid grid-cols-2 gap-2">
          <NumberField
            label={t("page.tools.menu_maker.base_width")}
            value={settings.baseWidth}
            min={640}
            max={7680}
            onChange={(value) => set("baseWidth", value)}
          />
          <NumberField
            label={t("page.tools.menu_maker.base_height")}
            value={settings.baseHeight}
            min={360}
            max={4320}
            onChange={(value) => set("baseHeight", value)}
          />
        </div>
        <div className="grid grid-cols-2 gap-2">
          <SettingField label={t("page.tools.menu_maker.fallback")}>
            <SettingSelect
              value={settings.fallbackType}
              items={FALLBACK_TYPES}
              onChange={(value) => set("fallbackType", value)}
              className="w-full"
            />
          </SettingField>
          <SettingField label={t("page.tools.menu_maker.button_alignment")}>
            <SettingSelect
              value={settings.slotAlignment}
              items={SLOT_ALIGNMENTS}
              onChange={(value) => set("slotAlignment", value)}
              className="w-full"
            />
          </SettingField>
        </div>
        <Toggle
          label={t("page.tools.menu_maker.remove_keys")}
          value={settings.removeOriginalKeys}
          onChange={(value) => set("removeOriginalKeys", value)}
        />
        <Toggle
          label={t("page.tools.menu_maker.key_hint")}
          value={settings.showKeyHint}
          onChange={(value) => set("showKeyHint", value)}
        />
        <Toggle
          label={t("page.tools.menu_maker.hide_upload_label")}
          value={settings.hideUploadLabel}
          onChange={(value) => set("hideUploadLabel", value)}
        />
        <Toggle
          label={t("page.tools.menu_maker.original_name")}
          value={settings.useOriginalININame}
          onChange={(value) => set("useOriginalININame", value)}
        />
        <Toggle
          label={t("page.tools.menu_maker.active_reset")}
          value={settings.resetActiveOnPresent}
          onChange={(value) => set("resetActiveOnPresent", value)}
        />
      </FieldSet>
      <FieldSet className="gap-2">
        <FieldLegend className="mb-2 text-xs font-semibold text-muted-foreground uppercase">
          {t("page.tools.menu_maker.palette")}
        </FieldLegend>
        <div className="grid grid-cols-2 gap-2">
          {(
            [
              "accent",
              "panelBackground",
              "panelBorder",
              "slotBackground",
              "slotHover",
              "slotBorder",
              "title",
              "titleShadow",
            ] as const
          ).map((key) => (
            <SettingField key={key} label={t(`page.tools.menu_maker.palette_${key}`)}>
              <Input
                type="color"
                value={settings.palette[key]}
                onChange={(event) => dispatch({ type: "palette", key, value: event.target.value })}
              />
            </SettingField>
          ))}
        </div>
        <div className="mt-2 grid grid-cols-2 gap-2">
          {(
            [
              "panelBackgroundAlpha",
              "panelBorderAlpha",
              "slotBackgroundAlpha",
              "slotHoverAlpha",
              "slotBorderAlpha",
            ] as const
          ).map((key) => (
            <NumberField
              key={key}
              label={t(`page.tools.menu_maker.palette_${key}`)}
              value={settings.palette[key]}
              min={0}
              max={255}
              onChange={(value) => dispatch({ type: "palette", key, value })}
            />
          ))}
        </div>
      </FieldSet>
    </div>
  );
}

export function PreviewSlot({
  slot,
  size,
  settings,
}: {
  slot: MenuMakerSlot;
  size: number;
  settings: MenuMakerSettings;
}) {
  return (
    <div
      className="flex min-w-0 flex-col items-center justify-center overflow-hidden rounded-sm border p-1"
      style={{
        width: size,
        height: size,
        color: slot.icon.color,
        textAlign: settings.slotAlignment,
        backgroundColor: alphaColor(
          settings.palette.slotBackground,
          settings.palette.slotBackgroundAlpha,
        ),
        borderColor: alphaColor(settings.palette.slotBorder, settings.palette.slotBorderAlpha),
      }}
    >
      {slot.icon.kind === "lucide" &&
      iconNames.includes(slot.icon.name as (typeof iconNames)[number]) ? (
        <DynamicIcon
          name={slot.icon.name as (typeof iconNames)[number]}
          style={{ width: size * 0.42, height: size * 0.42 }}
        />
      ) : slot.icon.kind === "upload" ? (
        <img src={slot.icon.dataUrl} className="h-[45%] w-[45%] rounded object-cover" />
      ) : slot.icon.kind === "iconify" ? (
        <span className="h-[45%] w-[45%]" dangerouslySetInnerHTML={{ __html: slot.icon.svg }} />
      ) : null}
      {!(slot.icon.kind === "upload" && settings.hideUploadLabel) && (
        <span
          className="mt-0.5 w-full truncate text-white"
          style={{ fontSize: Math.max(6, (size * 8) / MENU_MAKER_BASE_SLOT_SIZE) }}
        >
          {slot.name}
        </span>
      )}
      {settings.showKeyHint && (
        <span
          className="w-full truncate"
          style={{
            color: settings.palette.accent,
            fontSize: Math.max(5, (size * 7) / MENU_MAKER_BASE_SLOT_SIZE),
          }}
        >
          {slot.key}
        </span>
      )}
    </div>
  );
}

export function SlotEditor({
  slot,
  selected,
  settings,
  onSelect,
  onChange,
  onMove,
  onIcon,
  onUpload,
}: {
  slot: MenuMakerSlot;
  selected: boolean;
  settings: MenuMakerSettings;
  onSelect: (value: boolean) => void;
  onChange: (transform: (slot: MenuMakerSlot) => MenuMakerSlot) => void;
  onMove: (draggedId: string) => void;
  onIcon: () => void;
  onUpload: () => void;
}) {
  const { t } = useTranslation();
  return (
    <article
      draggable
      onDragStart={(event) => event.dataTransfer.setData("text/menu-maker-slot", slot.id)}
      onDragOver={(event) => event.preventDefault()}
      onDrop={(event) => {
        event.preventDefault();
        const draggedId = event.dataTransfer.getData("text/menu-maker-slot");
        if (draggedId) onMove(draggedId);
      }}
      className={cn(
        "group relative min-w-0 rounded border p-2",
        slot.skip && "opacity-40 grayscale",
      )}
      style={{
        minHeight: 116,
        backgroundColor: alphaColor(
          settings.palette.slotBackground,
          settings.palette.slotBackgroundAlpha,
        ),
        borderColor: settings.palette.slotBorder,
        textAlign: settings.slotAlignment,
      }}
    >
      <div className="flex items-center justify-between">
        <Checkbox
          checked={selected}
          onCheckedChange={(checked) => onSelect(checked === true)}
          aria-label={t("page.tools.menu_maker.select_slot")}
        />
        <span className="truncate text-[10px] text-muted-foreground">
          {(slot.handlers ?? []).length} handler
        </span>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="text-muted-foreground"
          onClick={() => onChange((value) => ({ ...value, skip: !value.skip }))}
        >
          {slot.skip ? t("page.tools.menu_maker.restore") : t("page.tools.menu_maker.skip")}
        </Button>
      </div>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="mx-auto my-1 size-9"
        style={{ color: slot.icon.color }}
        onClick={onIcon}
      >
        {slot.icon.kind === "lucide" &&
        iconNames.includes(slot.icon.name as (typeof iconNames)[number]) ? (
          <DynamicIcon name={slot.icon.name as (typeof iconNames)[number]} />
        ) : slot.icon.kind === "upload" ? (
          <img src={slot.icon.dataUrl} className="size-8 rounded object-cover" />
        ) : slot.icon.kind === "iconify" ? (
          <span className="size-8" dangerouslySetInnerHTML={{ __html: slot.icon.svg }} />
        ) : (
          <ImageIcon />
        )}
      </Button>
      <Input
        className="h-7 text-center text-xs"
        value={slot.name}
        onChange={(event) => onChange((value) => ({ ...value, name: event.target.value }))}
      />
      <div className="mt-1 flex gap-1">
        <Input
          className="h-6 min-w-0 px-1 text-center font-mono text-[10px]"
          value={slot.key}
          onChange={(event) => {
            try {
              onChange((value) => updateSlotKey(value, event.target.value));
            } catch {
              toast.error(t("page.tools.menu_maker.multi_key_locked"));
            }
          }}
        />
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={t("page.tools.menu_maker.upload_icon")}
                onClick={onUpload}
              >
                <UploadIcon className="size-3" />
              </Button>
            }
          />
          <TooltipContent>{t("page.tools.menu_maker.upload_icon")}</TooltipContent>
        </Tooltip>
      </div>
    </article>
  );
}

export function EmptyState({
  scan,
  onLoad,
  onIncludeTXT,
  t,
}: {
  scan?: MenuMakerScanResult;
  onLoad: (path: string) => Promise<void>;
  onIncludeTXT: () => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  return (
    <div className="mx-auto max-w-xl rounded-lg border border-dashed border-border bg-card p-6">
      <h2 className="mb-2 font-medium">{t("page.tools.menu_maker.empty_title")}</h2>
      {scan?.files?.length ? (
        <div className="space-y-2">
          {scan.files.map((file) => (
            <Button
              key={file.path}
              type="button"
              variant="outline"
              className="h-auto w-full justify-between px-3 py-3 text-left font-normal"
              onClick={() => void onLoad(file.path)}
            >
              <span className="truncate">{file.relativePath}</span>
              <span className="text-xs text-muted-foreground uppercase">{file.kind}</span>
            </Button>
          ))}
        </div>
      ) : (
        scan && (
          <Button className="mt-3" variant="outline" onClick={onIncludeTXT}>
            {t("page.tools.menu_maker.include_txt")}
          </Button>
        )
      )}
    </div>
  );
}

const FALLBACK_TYPES = [
  { value: "cycle", label: "cycle" },
  { value: "toggle", label: "toggle" },
  { value: "hold", label: "hold" },
  { value: "activate", label: "activate" },
] as const satisfies readonly { value: MenuMakerSettings["fallbackType"]; label: string }[];

const SLOT_ALIGNMENTS = [
  { value: "left", label: "left" },
  { value: "center", label: "center" },
  { value: "right", label: "right" },
] as const satisfies readonly { value: MenuMakerSettings["slotAlignment"]; label: string }[];

export function SettingSelect<T extends string>({
  value,
  items,
  onChange,
  disabled,
  className,
  size,
}: {
  value: T;
  items: readonly { value: T; label: string }[];
  onChange: (value: T) => void;
  disabled?: boolean;
  className?: string;
  size?: "sm" | "default";
}) {
  return (
    <Select
      value={value}
      items={[...items]}
      onValueChange={(next) => {
        if (next === null) return;
        onChange(next);
      }}
    >
      <SelectTrigger size={size} disabled={disabled} className={className}>
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

function SettingField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Field className="gap-1">
      <FieldLabel className="text-xs font-normal text-muted-foreground">{label}</FieldLabel>
      {children}
    </Field>
  );
}

function NumberField({
  label,
  value,
  min,
  max,
  step = 1,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step?: number;
  onChange: (value: number) => void;
}) {
  return (
    <SettingField label={label}>
      <Input
        type="number"
        value={value}
        min={min}
        max={max}
        step={step}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </SettingField>
  );
}

function Toggle({
  label,
  value,
  onChange,
}: {
  label: string;
  value: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <Field orientation="horizontal" className="items-center justify-between gap-3 py-1">
      <FieldLabel className="text-xs font-normal">{label}</FieldLabel>
      <Switch checked={value} onCheckedChange={onChange} />
    </Field>
  );
}

export function alphaColor(hex: string, alpha: number): string {
  return `${hex}${Math.round(Math.max(0, Math.min(255, alpha)))
    .toString(16)
    .padStart(2, "0")}`;
}

const CLICK_MODIFIERS = [
  { value: "alt", label: "alt" },
  { value: "ctrl", label: "ctrl" },
  { value: "shift", label: "shift" },
  { value: "none", label: "none" },
] as const satisfies readonly { value: MenuMakerSettings["clickModifier"]; label: string }[];
