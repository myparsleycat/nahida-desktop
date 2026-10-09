import { ReShade, type PresetEffects } from "@bindings/reshade";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { toErrorMessage } from "@shared/utils";
import { useQueryClient } from "@tanstack/react-query";
import { DownloadIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// The dialog stays open while effects is set. A failed install still reaches onInstalled, so a
// launch that waits on it goes ahead with the effects already there.
export function ReShadePresetEffectsDialog({
  effects,
  confirmLabel,
  skipLabel,
  onInstalled,
  onSkip,
  onClose,
}: {
  effects: PresetEffects | null;
  confirmLabel: string;
  skipLabel?: string;
  onInstalled: () => Promise<void> | void;
  onSkip?: () => Promise<void> | void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const packages = effects?.packages ?? [];
  const unknown = effects?.unknown ?? [];

  return (
    <Dialog open={effects !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("page.setting.xxmi.builtin.reshade.presetEffectsTitle")}</DialogTitle>
          <DialogDescription>
            {t(
              packages.length
                ? "page.setting.xxmi.builtin.reshade.presetEffectsDescription"
                : "page.setting.xxmi.builtin.reshade.presetEffectsUnknownOnly",
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-96 space-y-3 overflow-y-auto">
          {packages.map((pkg) => (
            <div key={pkg.id} className="space-y-0.5 rounded-lg bg-muted/50 p-3">
              <p className="text-sm font-medium">{pkg.name}</p>
              {pkg.description && (
                <p className="text-xs text-muted-foreground">{pkg.description}</p>
              )}
            </div>
          ))}
          {unknown.length > 0 && (
            <div className="space-y-1">
              {packages.length > 0 && (
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.xxmi.builtin.reshade.presetEffectsUnknown")}
                </p>
              )}
              <p className="font-mono text-xs break-all">{unknown.join(", ")}</p>
            </div>
          )}
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="outline" onClick={onClose}>
            {t(packages.length || onSkip ? "g.cancel" : "g.close")}
          </Button>
          {onSkip && (
            <Button
              variant={packages.length ? "outline" : "default"}
              onClickPromise={async () => onSkip()}
            >
              {skipLabel}
            </Button>
          )}
          {packages.length > 0 && (
            <Button
              onClickPromise={async () => {
                try {
                  await ReShade.InstallEffectPackages(packages.map((pkg) => pkg.id));
                } catch (error) {
                  toast.error(toErrorMessage(error));
                }
                void queryClient.invalidateQueries({ queryKey: ["reshade:effects"] });
                await onInstalled();
              }}
            >
              <DownloadIcon />
              {confirmLabel}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
