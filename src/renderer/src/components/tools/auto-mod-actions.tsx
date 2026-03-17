import {
  createDefaultAutoModActionsImporterConfig,
  type AutoModActionsConfig,
  type AutoModActionsImporterConfig,
  type AutoModActionsRestoreResult,
} from "@shared/auto-mod-actions";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { sortBy } from "es-toolkit";
import { Bot, RotateCcw, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { Button } from "../ui/button";
import { Switch } from "../ui/switch";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "../ui/select";

function ImporterSection({
  importerKey,
  importerConfig,
  presetOptions,
  disabled,
  onChange,
  onRestore,
  restoreDisabled,
}: {
  importerKey: string;
  importerConfig: AutoModActionsImporterConfig;
  presetOptions: { id: string; name: string }[];
  disabled: boolean;
  onChange: (nextConfig: AutoModActionsImporterConfig) => void;
  onRestore: () => void;
  restoreDisabled: boolean;
}) {
  const presetValue = importerConfig.autoFixer.presetId ?? "__none__";
  const isGIMI = importerKey.toUpperCase() === "GIMI";

  return (
    <section className="rounded-xl border bg-card p-4 space-y-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <span className="inline-flex h-8 w-8 items-center justify-center rounded-lg bg-accent/15 text-accent">
              <Sparkles className="h-4 w-4" />
            </span>
            <div>
              <h3 className="text-base font-semibold">{importerKey}</h3>
              <p className="text-sm text-muted-foreground">
                Configure importer-specific automation for newly detected mods.
              </p>
            </div>
          </div>
        </div>
        <Button variant="outline" size="sm" disabled={restoreDisabled} onClick={onRestore}>
          <RotateCcw className="h-4 w-4" />
          Restore Backups
        </Button>
      </div>

      <div className="rounded-lg border p-3 space-y-3">
        <div className="flex items-center justify-between gap-3">
          <div>
            <p className="text-sm font-medium">Auto Fixer</p>
            <p className="text-sm text-muted-foreground">
              Run the selected fix tool preset in ghost mode before built-in actions.
            </p>
          </div>
          <Switch
            checked={importerConfig.autoFixer.enabled}
            disabled={disabled}
            onCheckedChange={(checked) =>
              onChange({
                ...importerConfig,
                autoFixer: {
                  ...importerConfig.autoFixer,
                  enabled: checked,
                },
              })
            }
          />
        </div>

        <div className="space-y-1">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Preset
          </p>
          <Select
            disabled={disabled}
            value={presetValue}
            onValueChange={(value) =>
              onChange({
                ...importerConfig,
                autoFixer: {
                  ...importerConfig.autoFixer,
                  presetId: value === "__none__" ? null : value,
                },
              })
            }
          >
            <SelectTrigger className="w-full">
              <SelectValue placeholder="Select a preset" />
            </SelectTrigger>
            <SelectContent position="popper">
              <SelectGroup>
                <SelectLabel>Fix Tool Presets</SelectLabel>
                <SelectItem value="__none__">No preset</SelectItem>
                {presetOptions.map((preset) => (
                  <SelectItem key={preset.id} value={preset.id}>
                    {preset.name}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
      </div>

      {isGIMI && (
        <>
          <div className="rounded-lg border p-3 flex items-center justify-between gap-3">
            <div>
              <p className="text-sm font-medium">ORFix</p>
              <p className="text-sm text-muted-foreground">
                Append ORFix or NNFix to matching GIMI command list sections.
              </p>
            </div>
            <Switch
              checked={importerConfig.orFix?.enabled === true}
              disabled={disabled}
              onCheckedChange={(checked) =>
                onChange({
                  ...importerConfig,
                  orFix: { enabled: checked },
                })
              }
            />
          </div>

          <div className="rounded-lg border p-3 flex items-center justify-between gap-3">
            <div>
              <p className="text-sm font-medium">Face Head Fix</p>
              <p className="text-sm text-muted-foreground">
                Rewrite `ps-t0` to `this` inside sections that target `FaceHead`.
              </p>
            </div>
            <Switch
              checked={importerConfig.faceHeadFix?.enabled === true}
              disabled={disabled}
              onCheckedChange={(checked) =>
                onChange({
                  ...importerConfig,
                  faceHeadFix: { enabled: checked },
                })
              }
            />
          </div>
        </>
      )}
    </section>
  );
}

export default function AutoModActions() {
  const queryClient = useQueryClient();

  const { data: xxmiData } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => window.api.invoke("xxmi:getXXMIData"),
  });

  const { data: config, isPending: isConfigPending } = useQuery({
    queryKey: ["setting:xxmi:getAutoModActionsConfig"],
    queryFn: () => window.api.invoke("setting:xxmi:getAutoModActionsConfig"),
  });

  const { data: presetOptions = [] } = useQuery({
    queryKey: ["ftm:presets"],
    queryFn: async () => sortBy(await window.api.invoke("ftm:getPresets"), ["name"]),
  });

  const { mutate, isPending: isSaving } = useMutation({
    mutationFn: (nextConfig: AutoModActionsConfig) =>
      window.api.invoke("setting:xxmi:setAutoModActionsConfig", nextConfig),
    onSuccess: (_, nextConfig) => {
      queryClient.setQueryData(["setting:xxmi:getAutoModActionsConfig"], nextConfig);
    },
    onError: (error) => {
      toast.error(error.message);
    },
  });

  const restoreMutation = useMutation({
    mutationFn: (importerKey: string) =>
      window.api.invoke("setting:xxmi:restoreAutoModActionsBackups", importerKey),
    onSuccess: (result: AutoModActionsRestoreResult) => {
      if (result.restoredFiles === 0) {
        toast.message(`${result.importerKey}: no backups found`);
        return;
      }

      toast.success(
        `${result.importerKey}: restored ${result.restoredFiles} file${result.restoredFiles === 1 ? "" : "s"}`,
      );
    },
    onError: (error) => {
      toast.error(error.message);
    },
  });

  if (!xxmiData?.xxmiPath) {
    return (
      <div className="flex flex-col items-center justify-center w-full p-2 text-center">
        <h3 className="text-lg font-semibold text-muted-foreground">Auto Mod Actions</h3>
        <p className="text-sm text-muted-foreground italic">
          Configure XXMI first to enable importer automation.
        </p>
      </div>
    );
  }

  const importerKeys = (xxmiData.enabledImporters ?? []).map((importer) => importer.key);
  const effectiveConfig = config ?? {
    triggerMode: "startup+watch" as const,
    importers: Object.fromEntries(
      importerKeys.map((importerKey) => [
        importerKey,
        createDefaultAutoModActionsImporterConfig(importerKey),
      ]),
    ),
  };

  return (
    <div className="h-full min-h-0 overflow-y-auto">
      <div className="max-w-5xl space-y-4">
        <section className="rounded-2xl border bg-card p-5">
          <div className="flex items-start gap-3">
            <div className="inline-flex h-10 w-10 items-center justify-center rounded-xl bg-accent/15 text-accent">
              <Bot className="h-5 w-5" />
            </div>
            <div className="space-y-1">
              <h2 className="text-lg font-semibold">Auto Mod Actions</h2>
              <p className="text-sm text-muted-foreground">
                Trigger mode is fixed to startup plus folder watch. Existing mods are scanned once
                after XXMI initializes, then newly detected importer mods are processed
                automatically.
              </p>
            </div>
          </div>
        </section>

        {importerKeys.map((importerKey) => (
          <ImporterSection
            key={importerKey}
            importerKey={importerKey}
            importerConfig={
              effectiveConfig.importers[importerKey] ??
              createDefaultAutoModActionsImporterConfig(importerKey)
            }
            presetOptions={presetOptions}
            disabled={isConfigPending || isSaving}
            restoreDisabled={isConfigPending || isSaving || restoreMutation.isPending}
            onChange={(nextImporterConfig) =>
              mutate({
                ...effectiveConfig,
                importers: {
                  ...effectiveConfig.importers,
                  [importerKey]: nextImporterConfig,
                },
              })
            }
            onRestore={() => restoreMutation.mutate(importerKey)}
          />
        ))}
      </div>
    </div>
  );
}
