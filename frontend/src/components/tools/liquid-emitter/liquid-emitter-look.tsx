import type { Look } from "@bindings/tools/liquid_emitter";
import { InspectorSection } from "@renderer/components/tools/inspector-section";
import { RangeField } from "@renderer/components/tools/range-field";
import { Button } from "@renderer/components/ui/button";
import { ButtonGroup } from "@renderer/components/ui/button-group";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Field, FieldTitle } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { isEqual, round } from "es-toolkit";
import { useState } from "react";
import { useTranslation } from "react-i18next";

// Slots.hlsl slotDrive and slotAmount and the backend accept forces from 0.15
// to 2.35 and amounts from 0.02 to 2. The amount slider moves on its base-10
// logarithm so that the thin and dripping flows below 1 get room.
const MIN_FORCE = 0.15;
const MAX_FORCE = 2.35;
const MIN_AMOUNT_LOG = Math.log10(0.02);
const MAX_AMOUNT_LOG = Math.log10(2);
const DEFAULT_SCALE = 1;
const DEFAULT_ABSORPTION_MM = 15;
// Slots.hlsl slotViscosity and the backend accept viscosities from 0.1 to 10;
// the slider moves on its base-10 logarithm so that 1 sits in the middle.
const MAX_VISCOSITY_LOG = 1;
const DEFAULT_VISCOSITY = 1;
// The backend's nozzle kinds, in Slots.hlsl NOZZLE_* order; each gives the
// force, amount and viscosity their own meaning.
const NOZZLES = ["spray", "jet", "ooze"] as const;
type Nozzle = (typeof NOZZLES)[number];
const DEFAULT_NOZZLE: Nozzle = "spray";
// A preset is not stored: it only sets the scattering and roughness, which
// the backend accepts from 0 to 1 and the sliders show as percentages.
export const MATERIAL_PRESETS = {
  clear: { scattering: 0, roughness: 0 },
  translucent: { scattering: 0.35, roughness: 0.2 },
  cloudy: { scattering: 0.85, roughness: 0.35 },
} as const;
type MaterialPreset = keyof typeof MATERIAL_PRESETS;
const PRESETS = Object.keys(MATERIAL_PRESETS) as MaterialPreset[];

export function LookSection({
  saved,
  draft,
  onDraftChange,
  locked,
}: {
  /** The emitter's stored look, or the one a new emitter starts from. */
  saved: Look;
  draft: Look | null;
  onDraftChange: (look: Look | null) => void;
  locked: boolean;
}) {
  const { t } = useTranslation();
  const look = draft ?? saved;
  const dirty = lookChanged(draft, saved);
  const update = (change: Partial<Look>) => onDraftChange({ ...look, ...change });
  const runtimeLook = followsRuntime(look);
  const nozzle = nozzleOf(look);
  const preset = materialPresetOf(look);

  return (
    <InspectorSection
      id="liquid-emitter:look"
      title={t("page.tools.liquid_emitter.look.title")}
      summary={t(
        dirty
          ? "page.tools.liquid_emitter.look.summary.unsaved"
          : runtimeLook
            ? "page.tools.liquid_emitter.look.summary.runtime"
            : "page.tools.liquid_emitter.look.summary.custom",
      )}
      className="px-4 py-3"
      panelClassName="space-y-3 pt-3"
    >
      <p className="text-xs text-muted-foreground">{t("page.tools.liquid_emitter.look.hint")}</p>
      <Field className="gap-1">
        <FieldTitle>{t("page.tools.liquid_emitter.look.nozzle.title")}</FieldTitle>
        <ButtonGroup className="w-full">
          {NOZZLES.map((value) => (
            <Button
              key={value}
              size="sm"
              className="flex-1"
              variant={nozzle === value ? "default" : "outline"}
              aria-pressed={nozzle === value}
              disabled={locked}
              onClick={() => update({ nozzle: value })}
            >
              {t(`page.tools.liquid_emitter.look.nozzle.${value}`)}
            </Button>
          ))}
        </ButtonGroup>
        <p className="text-xs text-muted-foreground">
          {t(`page.tools.liquid_emitter.look.nozzle.${nozzle}_hint`)}
        </p>
      </Field>
      <div className="space-y-1">
        <RangeField
          label={t("page.tools.liquid_emitter.look.force")}
          value={look.force}
          defaultValue={DEFAULT_SCALE}
          min={MIN_FORCE}
          max={MAX_FORCE}
          step={0.05}
          display={`×${look.force.toFixed(2)}`}
          disabled={locked}
          onChange={(value) => update({ force: value })}
        />
        <p className="text-xs text-muted-foreground">
          {t(`page.tools.liquid_emitter.look.hints.${nozzle}.force`)}
        </p>
      </div>
      <div className="space-y-1">
        <RangeField
          label={t("page.tools.liquid_emitter.look.amount")}
          value={Math.log10(look.amount)}
          defaultValue={0}
          min={MIN_AMOUNT_LOG}
          max={MAX_AMOUNT_LOG}
          step={0.01}
          display={`×${look.amount.toFixed(2)}`}
          disabled={locked}
          onChange={(value) => update({ amount: round(10 ** value, 2) })}
        />
        <p className="text-xs text-muted-foreground">
          {t(`page.tools.liquid_emitter.look.hints.${nozzle}.amount`)}
        </p>
      </div>
      <div className="space-y-1">
        <RangeField
          label={t("page.tools.liquid_emitter.look.viscosity")}
          value={Math.log10(look.viscosity)}
          defaultValue={0}
          min={-MAX_VISCOSITY_LOG}
          max={MAX_VISCOSITY_LOG}
          step={0.05}
          display={`×${look.viscosity.toFixed(2)}`}
          disabled={locked}
          onChange={(value) => update({ viscosity: round(10 ** value, 2) })}
        />
        <p className="text-xs text-muted-foreground">
          {t(`page.tools.liquid_emitter.look.hints.${nozzle}.viscosity`)}
        </p>
      </div>
      <label className="flex items-start gap-2 text-sm">
        <Checkbox
          checked={look.customColor}
          disabled={locked}
          onCheckedChange={(checked) => update({ customColor: checked === true })}
        />
        <span>
          {t("page.tools.liquid_emitter.look.custom_color")}
          <span className="mt-0.5 block text-xs text-muted-foreground">
            {t("page.tools.liquid_emitter.look.custom_color_hint")}
          </span>
        </span>
      </label>
      {look.customColor ? (
        <>
          <label className="flex items-center justify-between gap-2 text-sm">
            {t("page.tools.liquid_emitter.look.tint")}
            <Input
              type="color"
              className="w-16 shrink-0"
              disabled={locked}
              value={tintToHex(look.tint)}
              onChange={(event) => update({ tint: hexToTint(event.currentTarget.value) })}
            />
          </label>
          <div className="space-y-1">
            <RangeField
              label={t("page.tools.liquid_emitter.look.absorption")}
              value={look.absorptionMm}
              defaultValue={DEFAULT_ABSORPTION_MM}
              min={2}
              max={60}
              step={1}
              display={`${look.absorptionMm} mm`}
              disabled={locked}
              onChange={(value) => update({ absorptionMm: value })}
            />
            <p className="text-xs text-muted-foreground">
              {t("page.tools.liquid_emitter.look.absorption_hint")}
            </p>
          </div>
        </>
      ) : null}
      <Field className="gap-1">
        <FieldTitle>
          {t("page.tools.liquid_emitter.look.material.title")}
          {preset === null ? (
            <span className="ml-auto text-xs font-normal text-muted-foreground">
              {t("page.tools.liquid_emitter.look.material.preset.custom")}
            </span>
          ) : null}
        </FieldTitle>
        <ButtonGroup className="w-full">
          {PRESETS.map((value) => (
            <Button
              key={value}
              size="sm"
              className="flex-1"
              variant={preset === value ? "default" : "outline"}
              aria-pressed={preset === value}
              disabled={locked}
              onClick={() => update(MATERIAL_PRESETS[value])}
            >
              {t(`page.tools.liquid_emitter.look.material.preset.${value}`)}
            </Button>
          ))}
        </ButtonGroup>
        <p className="text-xs text-muted-foreground">
          {t("page.tools.liquid_emitter.look.material.hint")}
        </p>
      </Field>
      <div className="space-y-1">
        <RangeField
          label={t("page.tools.liquid_emitter.look.material.scattering")}
          value={Math.round(look.scattering * 100)}
          defaultValue={0}
          min={0}
          max={100}
          step={1}
          display={`${Math.round(look.scattering * 100)}%`}
          disabled={locked}
          onChange={(value) => update({ scattering: round(value / 100, 2) })}
        />
        <p className="text-xs text-muted-foreground">
          {t("page.tools.liquid_emitter.look.material.scattering_hint")}
        </p>
      </div>
      <div className="space-y-1">
        <RangeField
          label={t("page.tools.liquid_emitter.look.material.roughness")}
          value={Math.round(look.roughness * 100)}
          defaultValue={0}
          min={0}
          max={100}
          step={1}
          display={`${Math.round(look.roughness * 100)}%`}
          disabled={locked}
          onChange={(value) => update({ roughness: round(value / 100, 2) })}
        />
        <p className="text-xs text-muted-foreground">
          {t("page.tools.liquid_emitter.look.material.roughness_hint")}
        </p>
      </div>
      <Button
        size="sm"
        variant="outline"
        className="w-full"
        disabled={locked || runtimeLook}
        onClick={() =>
          update({
            force: DEFAULT_SCALE,
            amount: DEFAULT_SCALE,
            customColor: false,
            viscosity: DEFAULT_VISCOSITY,
            nozzle: DEFAULT_NOZZLE,
            ...MATERIAL_PRESETS.clear,
          })
        }
      >
        {t("page.tools.liquid_emitter.look.reset")}
      </Button>
    </InspectorSection>
  );
}

export function lookChanged(draft: Look | null, saved: Look | undefined) {
  return draft !== null && !isEqual(draft, saved);
}

// The unsaved look of the emitter key names, one emitter of one mod. Any
// other key drops it, so a draft never comes back after a switch nor carries
// over to an emitter that later reuses the id.
export function useLookDraft(key: string) {
  const [draft, setDraft] = useState<{ key: string; look: Look } | null>(null);
  if (draft !== null && draft.key !== key) setDraft(null);
  const setLook = (look: Look | null) => setDraft(look ? { key, look } : null);
  return [draft?.key === key ? draft.look : null, setLook] as const;
}

export function followsRuntime(look: Look) {
  return (
    look.force === DEFAULT_SCALE &&
    look.amount === DEFAULT_SCALE &&
    !look.customColor &&
    look.viscosity === DEFAULT_VISCOSITY &&
    nozzleOf(look) === DEFAULT_NOZZLE &&
    materialPresetOf(look) === "clear"
  );
}

function nozzleOf(look: Look): Nozzle {
  return NOZZLES.find((value) => value === look.nozzle) ?? DEFAULT_NOZZLE;
}

function materialPresetOf(look: Look): MaterialPreset | null {
  return (
    PRESETS.find(
      (value) =>
        MATERIAL_PRESETS[value].scattering === look.scattering &&
        MATERIAL_PRESETS[value].roughness === look.roughness,
    ) ?? null
  );
}

// Tint channels run from 0 to 1, as the runtime's own color setting stores them.
function tintToHex(tint: number[]) {
  const channels = [0, 1, 2].map((index) =>
    Math.round(Math.min(1, Math.max(0, tint[index])) * 255)
      .toString(16)
      .padStart(2, "0"),
  );
  return `#${channels.join("")}`;
}

function hexToTint(hex: string) {
  return [1, 3, 5].map((start) =>
    Number((parseInt(hex.slice(start, start + 2), 16) / 255).toFixed(3)),
  );
}
