import { cn } from "@renderer/lib/utils";
import { useEffect, useRef, useState } from "react";
import type { ModelViewerVariantManifest, VariableStateValue } from "./model-viewer-dialog-types";
import { modelViewerSourceToUrl } from "./model-viewer-session";

export function formatSliderValue(value: number): string {
  if (Number.isInteger(value)) {
    return String(value);
  }

  return value.toFixed(2).replace(/\.?0+$/, "");
}

export function VariantTile({
  variable,
  activeValue,
  slotPath,
  slotHoverPath,
  slotActivePath,
  disabled,
  onSelect,
}: {
  variable: ModelViewerVariantManifest["variables"][number];
  activeValue: VariableStateValue | undefined;
  slotPath?: string;
  slotHoverPath?: string;
  slotActivePath?: string;
  disabled?: boolean;
  onSelect: (variableId: string, value: VariableStateValue) => void;
}) {
  const isActive = String(activeValue) !== String(variable.defaultValue);
  const framePath = isActive ? slotActivePath || slotHoverPath || slotPath : slotPath;

  return (
    <button
      type="button"
      className={cn(
        "relative flex aspect-square min-h-20 items-end justify-center overflow-hidden rounded-md border bg-black/20 p-2 text-white transition",
        disabled ? "cursor-not-allowed opacity-60" : "hover:bg-black/30",
      )}
      disabled={disabled}
      aria-disabled={disabled}
      tabIndex={disabled ? -1 : 0}
      onClick={() => {
        if (disabled || variable.values.length === 0) {
          return;
        }
        const nextIndex =
          variable.values.findIndex((entry) => String(entry.value) === String(activeValue)) + 1;
        const next = variable.values[nextIndex % variable.values.length];
        if (!next) {
          return;
        }
        onSelect(variable.id, next.value);
      }}
    >
      {framePath ? (
        <img
          src={modelViewerSourceToUrl(framePath)}
          alt=""
          className="absolute inset-0 h-full w-full object-cover"
        />
      ) : null}
      {variable.iconPath ? (
        <img
          src={modelViewerSourceToUrl(variable.iconPath)}
          alt={variable.label}
          className="absolute inset-3 h-[calc(100%-24px)] w-[calc(100%-24px)] object-contain"
        />
      ) : null}
      <div className="relative z-10 rounded bg-black/60 px-2 py-1 text-[11px] leading-none">
        {variable.label}
      </div>
    </button>
  );
}

export function VariantSlider({
  variable,
  activeValue,
  disabled,
  realtime,
  onSelect,
}: {
  variable: ModelViewerVariantManifest["variables"][number];
  activeValue: VariableStateValue | undefined;
  disabled?: boolean;
  realtime?: boolean;
  onSelect: (variableId: string, value: VariableStateValue) => void;
}) {
  const slider = variable.slider;
  const fallbackValue =
    typeof variable.defaultValue === "number"
      ? variable.defaultValue
      : Number(variable.values[0]?.value ?? 0);
  const resolvedValue =
    typeof activeValue === "number" ? activeValue : Number(activeValue ?? fallbackValue);
  const [draftValue, setDraftValue] = useState(resolvedValue);
  const commitTimeoutRef = useRef<number | null>(null);

  useEffect(() => {
    setDraftValue(resolvedValue);
  }, [resolvedValue]);

  useEffect(() => {
    return () => {
      if (commitTimeoutRef.current !== null) {
        window.clearTimeout(commitTimeoutRef.current);
      }
    };
  }, []);

  if (!slider) {
    return null;
  }

  const scheduleCommit = (nextValue: number) => {
    if (commitTimeoutRef.current !== null) {
      window.clearTimeout(commitTimeoutRef.current);
    }
    commitTimeoutRef.current = window.setTimeout(() => {
      onSelect(variable.id, nextValue);
      commitTimeoutRef.current = null;
    }, 150);
  };

  return (
    <div className="rounded-md border bg-background/50 p-3">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          {variable.iconPath ? (
            <img
              src={modelViewerSourceToUrl(variable.iconPath)}
              className="size-8 rounded object-contain"
            />
          ) : null}
          <div className="text-sm font-medium">{variable.label}</div>
        </div>
        <div className="text-xs tabular-nums text-muted-foreground">
          {formatSliderValue(draftValue)}
        </div>
      </div>
      <input
        type="range"
        min={slider.min}
        max={slider.max}
        step={slider.step}
        value={draftValue}
        disabled={disabled}
        className={cn("w-full accent-primary", disabled && "cursor-not-allowed opacity-60")}
        onChange={(event) => {
          const nextValue = Number(event.currentTarget.value);
          setDraftValue(nextValue);
          if (!disabled) {
            if (realtime) {
              onSelect(variable.id, nextValue);
            } else {
              scheduleCommit(nextValue);
            }
          }
        }}
      />
      <div className="mt-2 flex items-center justify-between text-[11px] tabular-nums text-muted-foreground">
        <span>{formatSliderValue(slider.min)}</span>
        <span>{formatSliderValue(slider.max)}</span>
      </div>
    </div>
  );
}
