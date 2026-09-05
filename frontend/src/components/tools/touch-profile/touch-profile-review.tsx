import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@renderer/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Slider } from "@renderer/components/ui/slider";
import { Switch } from "@renderer/components/ui/switch";
import { cn } from "@renderer/lib/utils";
import {
  TOUCH_PHYSICS_PRESETS,
  TOUCH_PROFILE_MASK_CURVE_RANGE,
  TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE,
  TOUCH_PROFILE_MASK_STRENGTH_RANGE,
  TOUCH_PROFILE_SETTING_RANGES,
  type TouchMaskCoreAttenuation,
  type TouchPhysicsPreset,
  type TouchProfileAdvancedSettings,
  type TouchZoneSettings,
  type TouchZoneStrengthPreset,
} from "@shared/touch-profile-settings";
import { Loader2Icon, Maximize2Icon, Minimize2Icon, RotateCcwIcon } from "lucide-react";

import type { TouchProfileSession } from "./use-touch-profile-session";

import {
  ALL_ZONES,
  DEFAULT_MASK_STRENGTH,
  DEFAULT_MASK_CURVE,
  DEFAULT_MASK_RADIUS_SCALE,
} from "./touch-profile-defaults";
const DEFAULT_MASK_CORE_ATTENUATION = "off" as const;
function sourceLabel(source: "vision" | "manual" | "bone", t: (key: string) => string) {
  // vision-llm disabled — vision source branch kept for type completeness
  if (source === "vision") return t("page.tools.touch_profile.vision_source");
  if (source === "bone") return t("page.tools.touch_profile.mode_bone");
  return t("page.tools.touch_profile.manual_source");
}
const advancedSettingEntries = [
  ["radius", "page.tools.touch_profile.advanced_radius"],
  ["strength", "page.tools.touch_profile.advanced_strength"],
  ["damping", "page.tools.touch_profile.advanced_damping"],
  ["spring", "page.tools.touch_profile.advanced_spring"],
  ["maxOffset", "page.tools.touch_profile.advanced_max_offset"],
  ["falloff", "page.tools.touch_profile.advanced_falloff"],
] as const satisfies ReadonlyArray<[keyof TouchProfileAdvancedSettings, string]>;
function isTouchZoneStrengthPreset(value: string): value is TouchZoneStrengthPreset {
  return value === "light" || value === "normal" || value === "strong";
}
function isTouchPhysicsPreset(value: string): value is Exclude<TouchPhysicsPreset, "custom"> {
  return value === "soft" || value === "normal" || value === "firm";
}
function isTouchMaskCoreAttenuation(value: string | null): value is TouchMaskCoreAttenuation {
  return value === "off" || value === "linear" || value === "sqrt" || value === "pow";
}
function createPresetSettings(
  preset: Exclude<TouchPhysicsPreset, "custom">,
  strengthPreset: TouchZoneStrengthPreset,
  maskStrength: number,
  maskCurve: number,
  maskRadiusScale: number,
): TouchZoneSettings {
  return {
    maskStrength,
    maskCurve,
    maskRadiusScale,
    maskCoreAttenuation: "off",
    strengthPreset,
    physicsPreset: preset,
    advanced: { ...TOUCH_PHYSICS_PRESETS[preset] },
  };
}
function formatTouchSettingValue(value: number) {
  return Number.isInteger(value)
    ? String(value)
    : value.toFixed(3).replace(/0+$/, "").replace(/\.$/, "");
}
export function TouchProfileReview({ session }: { session: TouchProfileSession }) {
  const {
    t,
    applying,
    rollingBack,
    draft,
    result,
    setSelectedComponentId,
    setSelectedZoneId,
    linkedComponents,
    pendingSettingsSaves,
    interactiveComponents,
    activeComponentId,
    selectedComponent,
    activeZoneId,
    selectedZone,
    activePreview,
    backToSelect,
    applyDraft,
    updateZoneSettings,
    setComponentLinked,
    updateZoneAdvancedSetting,
    updateZoneMaskStrength,
    resetZoneMaskStrength,
    updateZoneMaskCurve,
    resetZoneMaskCurve,
    updateZoneMaskRadiusScale,
    previewLoading,
    previewError,
  } = session;

  if (!draft) return null;
  return (
    <>
      <div>
        <div className="text-sm font-medium">{t("page.tools.touch_profile.preview_title")}</div>
        <div className="mt-2 flex gap-3 text-xs text-muted-foreground">
          <span>
            {t("page.tools.touch_profile.support_grade")}: {draft.analysis.supportGrade}
          </span>
          <span>
            {t("page.tools.touch_profile.auto_apply")}:{" "}
            {draft.canAutoApply
              ? t("page.tools.touch_profile.yes")
              : t("page.tools.touch_profile.no")}
          </span>
        </div>
        {/* vision-llm disabled — LLM summary isolated */}
        {/* {draft.llm ? (
                      <div
                        className="mt-2 truncate text-xs text-muted-foreground"
                        title={draft.llm.endpoint}
                      >
                        {t("page.tools.touch_profile.llm_summary")}: {draft.llm.protocol} /{" "}
                        {draft.llm.model}
                      </div>
                    ) : null} */}
      </div>

      <Field>
        <FieldLabel>{t("page.tools.touch_profile.component")}</FieldLabel>
        <Select
          value={activeComponentId}
          items={interactiveComponents.map((component) => ({
            value: component.componentId,
            label: `${component.componentId} (${component.zones.length})`,
          }))}
          onValueChange={(value) => {
            if (value) {
              setSelectedComponentId(value);
              setSelectedZoneId(ALL_ZONES);
            }
          }}
          disabled={interactiveComponents.length === 0 || previewLoading}
        >
          <SelectTrigger className="w-full">
            <SelectValue placeholder={t("page.tools.touch_profile.component_placeholder")} />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {interactiveComponents.map((component) => (
                <SelectItem key={component.componentId} value={component.componentId}>
                  {component.componentId} ({component.zones.length})
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </Field>

      <Field>
        <FieldLabel>{t("page.tools.touch_profile.zone")}</FieldLabel>
        <Select
          value={activeZoneId}
          items={[
            { value: ALL_ZONES, label: t("page.tools.touch_profile.all_zones") },
            ...(activePreview?.zones ?? []).map((zone) => ({
              value: zone.id,
              label: zone.label || zone.id,
            })),
          ]}
          onValueChange={(value) => {
            if (value) setSelectedZoneId(value);
          }}
          disabled={!activePreview || previewLoading}
        >
          <SelectTrigger className="w-full">
            <SelectValue placeholder={t("page.tools.touch_profile.zone_placeholder")} />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value={ALL_ZONES}>{t("page.tools.touch_profile.all_zones")}</SelectItem>
              {(activePreview?.zones ?? []).map((zone) => (
                <SelectItem key={zone.id} value={zone.id}>
                  {zone.label || zone.id}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </Field>

      {selectedComponent ? (
        <div className="space-y-2 rounded-md border border-border/80 p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="text-sm font-medium">{selectedComponent.componentId}</div>
            <div className="flex items-center gap-2">
              <Badge variant="outline">
                {t("page.tools.touch_profile.confidence")}{" "}
                {(selectedComponent.confidence * 100).toFixed(0)}%
              </Badge>
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <span>{t("page.tools.touch_profile.link_settings")}</span>
                <Switch
                  size="sm"
                  checked={linkedComponents[selectedComponent.componentId] ?? true}
                  onCheckedChange={(checked) =>
                    setComponentLinked(selectedComponent.componentId, checked)
                  }
                  aria-label={t("page.tools.touch_profile.link_settings")}
                />
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2 rounded bg-muted/40 p-2 text-xs">
            <div className="flex items-center gap-2">
              <span className="font-medium text-foreground">
                {t("page.tools.touch_profile.turn_label")}: Turn{" "}
                {selectedComponent.currentTurn ?? 1}
              </span>
              {/* vision-llm disabled — turn history select isolated */}
              {/* {selectedComponent.turnHistory &&
                          selectedComponent.turnHistory.length > 1 ? (
                            <Select
                              value={String(selectedComponent.currentTurn ?? 1)}
                              onValueChange={(val) => {
                                if (val)
                                  void selectTurn(selectedComponent.componentId, Number(val));
                              }}
                              disabled={reanalyzing || loading}
                            >
                              <SelectTrigger className="h-7 text-xs">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectGroup>
                                  {selectedComponent.turnHistory.map((item) => (
                                    <SelectItem key={item.turn} value={String(item.turn)}>
                                      Turn {item.turn} {item.approved ? " (LGTM)" : ""}
                                    </SelectItem>
                                  ))}
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                          ) : null} */}
            </div>
            {/* vision-llm disabled — reanalyze button isolated */}
            {/* <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs"
                          onClick={() => void reanalyzeTurn(selectedComponent.componentId)}
                          disabled={reanalyzing || loading}
                        >
                          {reanalyzing ? (
                            <Loader2Icon className="mr-1.5 size-3.5 animate-spin" />
                          ) : (
                            <SparklesIcon className="mr-1.5 size-3.5" />
                          )}
                          {reanalyzing
                            ? t("page.tools.touch_profile.reanalyzing")
                            : t("page.tools.touch_profile.reanalyze")}
                        </Button> */}
          </div>
          {selectedComponent.zones
            .filter(
              (_, index) =>
                !(linkedComponents[selectedComponent.componentId] ?? true) || index === 0,
            )
            .map((zone) => {
              const source = sourceLabel(zone.source, t);
              const isSelected = activeZoneId === zone.id;
              const isLinked = linkedComponents[selectedComponent.componentId] ?? true;
              const linkedZoneIds = isLinked
                ? selectedComponent.zones.map((item) => item.id)
                : undefined;
              return (
                <div
                  key={zone.id}
                  className={cn(
                    "w-full rounded-md border px-3 py-2 text-left transition-colors",
                    !isLinked && isSelected
                      ? "border-primary bg-primary/10"
                      : "border-border/60 hover:bg-muted/60",
                  )}
                >
                  {isLinked ? (
                    <div className="text-xs text-muted-foreground">
                      {selectedComponent.zones.map((item) => item.label || item.id).join(" · ")}
                    </div>
                  ) : (
                    <button
                      type="button"
                      className="w-full text-left"
                      onClick={() => setSelectedZoneId(isSelected ? ALL_ZONES : zone.id)}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-sm font-medium">{zone.label || zone.id}</span>
                        <Badge variant="outline">{source}</Badge>
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        {t("page.tools.touch_profile.confidence")}{" "}
                        {(zone.confidence * 100).toFixed(0)}%
                      </div>
                    </button>
                  )}
                  <div className="mt-3 grid gap-2 sm:grid-cols-2">
                    <Field>
                      <FieldLabel>{t("page.tools.touch_profile.touch_strength")}</FieldLabel>
                      <Select
                        value={zone.settings.strengthPreset}
                        items={[
                          {
                            value: "light",
                            label: t("page.tools.touch_profile.strength_light"),
                          },
                          {
                            value: "normal",
                            label: t("page.tools.touch_profile.strength_normal"),
                          },
                          {
                            value: "strong",
                            label: t("page.tools.touch_profile.strength_strong"),
                          },
                        ]}
                        onValueChange={(value) => {
                          if (typeof value === "string" && isTouchZoneStrengthPreset(value)) {
                            updateZoneSettings(
                              selectedComponent.componentId,
                              zone.id,
                              {
                                ...zone.settings,
                                strengthPreset: value,
                              },
                              { zoneIds: linkedZoneIds },
                            );
                          }
                        }}
                      >
                        <SelectTrigger className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value="light">
                              {t("page.tools.touch_profile.strength_light")}
                            </SelectItem>
                            <SelectItem value="normal">
                              {t("page.tools.touch_profile.strength_normal")}
                            </SelectItem>
                            <SelectItem value="strong">
                              {t("page.tools.touch_profile.strength_strong")}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    </Field>
                    <Field>
                      <FieldLabel>{t("page.tools.touch_profile.physics_preset")}</FieldLabel>
                      <Select
                        value={zone.settings.physicsPreset}
                        items={[
                          {
                            value: "soft",
                            label: t("page.tools.touch_profile.physics_soft"),
                          },
                          {
                            value: "normal",
                            label: t("page.tools.touch_profile.physics_normal"),
                          },
                          {
                            value: "firm",
                            label: t("page.tools.touch_profile.physics_firm"),
                          },
                          ...(zone.settings.physicsPreset === "custom"
                            ? [
                                {
                                  value: "custom",
                                  label: t("page.tools.touch_profile.physics_custom"),
                                },
                              ]
                            : []),
                        ]}
                        onValueChange={(value) => {
                          if (typeof value === "string" && isTouchPhysicsPreset(value)) {
                            updateZoneSettings(
                              selectedComponent.componentId,
                              zone.id,
                              createPresetSettings(
                                value,
                                zone.settings.strengthPreset,
                                zone.settings.maskStrength ?? DEFAULT_MASK_STRENGTH,
                                zone.settings.maskCurve ?? DEFAULT_MASK_CURVE,
                                zone.settings.maskRadiusScale ?? DEFAULT_MASK_RADIUS_SCALE,
                              ),
                              { zoneIds: linkedZoneIds },
                            );
                          }
                        }}
                      >
                        <SelectTrigger className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value="soft">
                              {t("page.tools.touch_profile.physics_soft")}
                            </SelectItem>
                            <SelectItem value="normal">
                              {t("page.tools.touch_profile.physics_normal")}
                            </SelectItem>
                            <SelectItem value="firm">
                              {t("page.tools.touch_profile.physics_firm")}
                            </SelectItem>
                            {zone.settings.physicsPreset === "custom" ? (
                              <SelectItem value="custom">
                                {t("page.tools.touch_profile.physics_custom")}
                              </SelectItem>
                            ) : null}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    </Field>
                  </div>
                  <div className="mt-3">
                    <div className="mb-1.5 flex items-center justify-between gap-2">
                      <span className="text-sm font-medium">
                        {t("page.tools.touch_profile.mask_radius")}
                      </span>
                      <span className="text-xs text-muted-foreground tabular-nums">
                        {Math.round(
                          (zone.settings.maskRadiusScale ?? DEFAULT_MASK_RADIUS_SCALE) * 100,
                        )}
                        %
                      </span>
                    </div>
                    <div className="grid grid-cols-2 gap-2">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="h-8 text-xs"
                        onClick={() =>
                          updateZoneMaskRadiusScale(
                            selectedComponent.componentId,
                            zone,
                            -TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.step,
                            { zoneIds: linkedZoneIds },
                          )
                        }
                        disabled={
                          applying ||
                          rollingBack ||
                          (zone.settings.maskRadiusScale ?? DEFAULT_MASK_RADIUS_SCALE) <=
                            TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.min
                        }
                      >
                        <Minimize2Icon className="mr-1.5 size-3.5" />
                        {t("page.tools.touch_profile.mask_radius_shrink")}
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="h-8 text-xs"
                        onClick={() =>
                          updateZoneMaskRadiusScale(
                            selectedComponent.componentId,
                            zone,
                            TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.step,
                            { zoneIds: linkedZoneIds },
                          )
                        }
                        disabled={
                          applying ||
                          rollingBack ||
                          (zone.settings.maskRadiusScale ?? DEFAULT_MASK_RADIUS_SCALE) >=
                            TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.max
                        }
                      >
                        <Maximize2Icon className="mr-1.5 size-3.5" />
                        {t("page.tools.touch_profile.mask_radius_grow")}
                      </Button>
                    </div>
                  </div>
                  <Field className="mt-3">
                    <FieldLabel>{t("page.tools.touch_profile.mask_core_attenuation")}</FieldLabel>
                    <Select
                      value={zone.settings.maskCoreAttenuation ?? DEFAULT_MASK_CORE_ATTENUATION}
                      items={[
                        {
                          value: "off",
                          label: t("page.tools.touch_profile.mask_core_attenuation_off"),
                        },
                        {
                          value: "linear",
                          label: t("page.tools.touch_profile.mask_core_attenuation_linear"),
                        },
                        {
                          value: "sqrt",
                          label: t("page.tools.touch_profile.mask_core_attenuation_sqrt"),
                        },
                        {
                          value: "pow",
                          label: t("page.tools.touch_profile.mask_core_attenuation_pow"),
                        },
                      ]}
                      onValueChange={(value) => {
                        if (isTouchMaskCoreAttenuation(value)) {
                          updateZoneSettings(
                            selectedComponent.componentId,
                            zone.id,
                            { ...zone.settings, maskCoreAttenuation: value },
                            { zoneIds: linkedZoneIds, refreshPreview: true },
                          );
                        }
                      }}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value="off">
                            {t("page.tools.touch_profile.mask_core_attenuation_off")}
                          </SelectItem>
                          <SelectItem value="linear">
                            {t("page.tools.touch_profile.mask_core_attenuation_linear")}
                          </SelectItem>
                          <SelectItem value="sqrt">
                            {t("page.tools.touch_profile.mask_core_attenuation_sqrt")}
                          </SelectItem>
                          <SelectItem value="pow">
                            {t("page.tools.touch_profile.mask_core_attenuation_pow")}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FieldDescription>
                      {t("page.tools.touch_profile.mask_core_attenuation_hint")}
                    </FieldDescription>
                  </Field>
                  <Field className="mt-3">
                    <div className="flex items-center justify-between gap-2">
                      <FieldLabel>{t("page.tools.touch_profile.mask_strength")}</FieldLabel>
                      <div className="flex items-center gap-1">
                        <span className="text-xs text-muted-foreground tabular-nums">
                          {Math.round((zone.settings.maskStrength ?? DEFAULT_MASK_STRENGTH) * 100)}%
                        </span>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="size-6"
                          title={t("page.tools.touch_profile.mask_strength_reset")}
                          aria-label={t("page.tools.touch_profile.mask_strength_reset")}
                          onClick={() =>
                            resetZoneMaskStrength(selectedComponent.componentId, zone, {
                              zoneIds: linkedZoneIds,
                            })
                          }
                        >
                          <RotateCcwIcon className="size-3.5" />
                        </Button>
                      </div>
                    </div>
                    <Slider
                      min={TOUCH_PROFILE_MASK_STRENGTH_RANGE.min}
                      max={TOUCH_PROFILE_MASK_STRENGTH_RANGE.max}
                      step={TOUCH_PROFILE_MASK_STRENGTH_RANGE.step}
                      value={zone.settings.maskStrength ?? DEFAULT_MASK_STRENGTH}
                      onValueChange={(next) => {
                        const nextValue = Array.isArray(next) ? next[0] : next;
                        if (typeof nextValue === "number") {
                          updateZoneMaskStrength(selectedComponent.componentId, zone, nextValue, {
                            zoneIds: linkedZoneIds,
                          });
                        }
                      }}
                    />
                  </Field>
                  <Field className="mt-3">
                    <div className="flex items-center justify-between gap-2">
                      <FieldLabel>{t("page.tools.touch_profile.mask_curve")}</FieldLabel>
                      <div className="flex items-center gap-1">
                        <span className="text-xs text-muted-foreground tabular-nums">
                          {formatTouchSettingValue(zone.settings.maskCurve ?? DEFAULT_MASK_CURVE)}x
                        </span>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="size-6"
                          title={t("page.tools.touch_profile.mask_curve_reset")}
                          aria-label={t("page.tools.touch_profile.mask_curve_reset")}
                          onClick={() =>
                            resetZoneMaskCurve(selectedComponent.componentId, zone, {
                              zoneIds: linkedZoneIds,
                            })
                          }
                        >
                          <RotateCcwIcon className="size-3.5" />
                        </Button>
                      </div>
                    </div>
                    <div className="mb-2 text-xs text-muted-foreground">
                      {t("page.tools.touch_profile.mask_curve_hint")}
                    </div>
                    <Slider
                      min={TOUCH_PROFILE_MASK_CURVE_RANGE.min}
                      max={TOUCH_PROFILE_MASK_CURVE_RANGE.max}
                      step={TOUCH_PROFILE_MASK_CURVE_RANGE.step}
                      value={zone.settings.maskCurve ?? DEFAULT_MASK_CURVE}
                      onValueChange={(next) => {
                        const nextValue = Array.isArray(next) ? next[0] : next;
                        if (typeof nextValue === "number") {
                          updateZoneMaskCurve(selectedComponent.componentId, zone, nextValue, {
                            zoneIds: linkedZoneIds,
                          });
                        }
                      }}
                    />
                  </Field>
                </div>
              );
            })}
        </div>
      ) : (
        <div className="rounded-md border border-border/80 px-3 py-2 text-xs text-muted-foreground">
          {t("page.tools.touch_profile.no_interactive_components")}
        </div>
      )}

      {selectedZone &&
      activeZoneId !== ALL_ZONES &&
      selectedComponent &&
      !(linkedComponents[selectedComponent.componentId] ?? true) ? (
        <div className="rounded-md border border-border/80 p-3">
          <div className="text-sm font-medium">
            {t("page.tools.touch_profile.advanced_title")}: {selectedZone.label}
          </div>
          <div className="mt-1 text-xs text-muted-foreground">
            {t("page.tools.touch_profile.advanced_description")}
          </div>
          <div className="mt-3 grid gap-4">
            {advancedSettingEntries.map(([key, labelKey]) => {
              const range = TOUCH_PROFILE_SETTING_RANGES[key];
              const value = selectedZone.settings.advanced[key];
              return (
                <Field key={key}>
                  <div className="flex items-center justify-between gap-2">
                    <FieldLabel>{t(labelKey)}</FieldLabel>
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {formatTouchSettingValue(value)}
                    </span>
                  </div>
                  <Slider
                    min={range.min}
                    max={range.max}
                    step={range.step}
                    value={value}
                    onValueChange={(next) => {
                      const nextValue = Array.isArray(next) ? next[0] : next;
                      if (typeof nextValue === "number") {
                        updateZoneAdvancedSetting(
                          selectedComponent.componentId,
                          selectedZone,
                          key,
                          nextValue,
                        );
                      }
                    }}
                  />
                </Field>
              );
            })}
          </div>
        </div>
      ) : null}

      {previewError ? (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {t("page.tools.touch_profile.preview_failed")}: {previewError}
        </div>
      ) : null}

      {selectedComponent?.warnings.length ? (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
          {selectedComponent.warnings.join(" · ")}
        </div>
      ) : null}

      {draft.warnings.length > 0 ? (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
          {draft.warnings.join(" · ")}
        </div>
      ) : null}

      <Button type="button" variant="outline" onClick={backToSelect} disabled={applying}>
        {t("page.tools.touch_profile.mesh_back")}
      </Button>

      <Button
        type="button"
        onClick={() => void applyDraft(!draft.canAutoApply)}
        disabled={applying || pendingSettingsSaves > 0 || interactiveComponents.length === 0}
      >
        {applying ? <Loader2Icon className="mr-2 size-4 animate-spin" /> : null}
        {result
          ? t("page.tools.touch_profile.regenerate")
          : draft.canAutoApply
            ? t("page.tools.touch_profile.create")
            : t("page.tools.touch_profile.force_create")}
      </Button>
    </>
  );
}
