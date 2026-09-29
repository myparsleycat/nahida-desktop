import { Dialog } from "@bindings/platform";
import { Button } from "@renderer/components/ui/button";
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
import { cn } from "@renderer/lib/utils";
import { toErrorMessage } from "@shared/utils";
import { FolderOpenIcon } from "lucide-react";
import { useId, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function FieldLabel({ label, description }: { label: ReactNode; description?: ReactNode }) {
  return (
    <span className="min-w-0 space-y-0.5">
      <span className="block text-sm font-medium">{label}</span>
      {description && <span className="block text-xs text-muted-foreground">{description}</span>}
    </span>
  );
}

// Nested options appear only while the parent toggle is on, so disabled inputs never pile up.
export function ToggleRow({
  label,
  description,
  checked,
  disabled,
  onCheckedChange,
  children,
}: {
  label: ReactNode;
  description?: ReactNode;
  checked: boolean;
  disabled?: boolean;
  onCheckedChange: (checked: boolean) => void;
  children?: ReactNode;
}) {
  return (
    <div className="space-y-3">
      <label className="flex items-center justify-between gap-4">
        <FieldLabel label={label} description={description} />
        <Switch checked={checked} disabled={disabled} onCheckedChange={onCheckedChange} />
      </label>
      {checked && children && <div className="space-y-3 border-l-2 pl-4">{children}</div>}
    </div>
  );
}

export function SelectRow({
  label,
  description,
  value,
  options,
  onValueChange,
}: {
  label: ReactNode;
  description?: ReactNode;
  value: string;
  options: readonly (string | { value: string; label: string })[];
  onValueChange: (value: string) => void;
}) {
  const labelId = useId();
  const items = options.map((option) =>
    typeof option === "string" ? { value: option, label: option } : option,
  );

  return (
    <div className="flex items-center justify-between gap-4">
      <span id={labelId}>
        <FieldLabel label={label} description={description} />
      </span>
      <Select
        value={value}
        items={items}
        onValueChange={(next) => {
          if (next !== null) onValueChange(next);
        }}
      >
        <SelectTrigger className="w-44 shrink-0" aria-labelledby={labelId}>
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
    </div>
  );
}

export function NumberRow({
  label,
  description,
  value,
  min,
  max,
  step,
  onValueChange,
}: {
  label: ReactNode;
  description?: ReactNode;
  value: number;
  min?: number;
  max?: number;
  step?: number | "any";
  onValueChange: (value: number) => void;
}) {
  return (
    <label className="flex items-center justify-between gap-4">
      <FieldLabel label={label} description={description} />
      <Input
        type="number"
        className="w-28 shrink-0 text-right"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(event) => onValueChange(Number(event.target.value))}
      />
    </label>
  );
}

export function PathField({
  label,
  description,
  value,
  onValueChange,
  className,
  children,
}: {
  label: ReactNode;
  description?: ReactNode;
  value: string;
  onValueChange: (value: string) => void;
  className?: string;
  children?: ReactNode;
}) {
  const { t } = useTranslation();
  const inputId = useId();

  return (
    <div className={cn("space-y-1.5", className)}>
      <label htmlFor={inputId}>
        <FieldLabel label={label} description={description} />
      </label>
      <div className="flex gap-2">
        <Input
          id={inputId}
          value={value}
          spellCheck={false}
          onChange={(event) => onValueChange(event.target.value)}
        />
        <Button
          variant="outline"
          size="icon"
          aria-label={t("page.setting.xxmi.builtin.browse")}
          title={t("page.setting.xxmi.builtin.browse")}
          onClickPromise={async () => {
            try {
              const selected = await Dialog.ShowOpenDialog({
                title: typeof label === "string" ? label : "",
                defaultPath: value,
                filters: [],
                properties: ["openDirectory"],
              });
              if (!selected.canceled && selected.filePaths?.[0]) {
                onValueChange(selected.filePaths[0]);
              }
            } catch (error) {
              toast.error(toErrorMessage(error));
            }
          }}
        >
          <FolderOpenIcon />
        </Button>
        {children}
      </div>
    </div>
  );
}
