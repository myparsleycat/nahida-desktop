import { Field, FieldLabel } from "@renderer/components/ui/field";

export function RangeField({
  label,
  value,
  defaultValue,
  min,
  max,
  step,
  display,
  disabled,
  onChange,
}: {
  label: string;
  value: number;
  defaultValue: number;
  min: number;
  max: number;
  step: number;
  display: string;
  disabled?: boolean;
  onChange: (value: number) => void;
}) {
  return (
    <Field>
      <div className="mb-1 flex items-center justify-between gap-2">
        <FieldLabel className="mb-0">{label}</FieldLabel>
        <span className="text-[11px] text-muted-foreground tabular-nums">{display}</span>
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        disabled={disabled}
        className="w-full accent-primary disabled:cursor-not-allowed disabled:opacity-50"
        onPointerDown={(event) => {
          // Chromium still sends pointer events to a disabled input.
          if (disabled || !(event.ctrlKey || event.metaKey)) return;
          event.preventDefault();
          onChange(defaultValue);
        }}
        onChange={(event) => onChange(Number(event.currentTarget.value))}
      />
    </Field>
  );
}
