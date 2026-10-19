import { Manager as ModRuntime, type Status } from "@bindings/modruntime";
import { Shell } from "@bindings/platform";
import { KeyRecorder } from "@renderer/components/mod/key-recorder";
import { Alert, AlertDescription } from "@renderer/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Kbd } from "@renderer/components/ui/kbd";
import {
  Section,
  SectionContent,
  SectionDescription,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import { NumberRow, ToggleRow } from "@renderer/components/xxmi/xxmi-fields";
import { formatKeyLabel } from "@shared/key-formatter";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { isEqual } from "es-toolkit";
import { FolderOpenIcon, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const RUNTIME_GROUP = "nhd-runtime";
const errorCodePattern = /(MOD_RUNTIME_[A-Z_]+)(?::([^\n]*))?/;
const enabledStates = ["current", "outdated", "modified"];
const installedStates = [...enabledStates, "disabled"];

export function modRuntimesQueryKey(importer: string) {
  return ["mod-runtime", importer] as const;
}

export function XXMIModRuntimes({ importer }: { importer: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data: listing } = useQuery({
    queryKey: modRuntimesQueryKey(importer),
    queryFn: () => ModRuntime.List(importer),
  });
  const [removing, setRemoving] = useState<Status | null>(null);

  if (!listing) return null;

  const run = async (
    action: (importer: string, name: string) => Promise<Status>,
    runtime: Status,
    success: string,
  ) => {
    try {
      await action(importer, runtime.name);
      toast.success(t(success, { name: runtime.name }));
      return true;
    } catch (error) {
      const message = toErrorMessage(error);
      const match = errorCodePattern.exec(message);
      toast.error(
        match
          ? t(`page.setting.xxmi.modRuntimes.errors.${match[1]}`, {
              detail: match[2] ?? "",
              defaultValue: message,
            })
          : message,
      );
      return false;
    } finally {
      // Installing or removing a runtime adds or drops a mod in the library, and the tools read its state.
      void queryClient.invalidateQueries({ queryKey: modRuntimesQueryKey(importer) });
      void queryClient.invalidateQueries({ queryKey: ["modGroup"] });
      void queryClient.invalidateQueries({ queryKey: ["characters"] });
      void queryClient.invalidateQueries({ queryKey: ["tools:liquidEmitter"] });
    }
  };

  return (
    <Section>
      <SectionHeader>
        <SectionTitle>{t("page.setting.xxmi.modRuntimes.title")}</SectionTitle>
        <SectionDescription>
          {t("page.setting.xxmi.modRuntimes.description", { folder: RUNTIME_GROUP })}
        </SectionDescription>
      </SectionHeader>
      <SectionContent>
        {!listing.modsRoot ? (
          <Alert>
            <TriangleAlertIcon />
            <AlertDescription>{t("page.setting.xxmi.modRuntimes.noModsFolder")}</AlertDescription>
          </Alert>
        ) : null}
        {listing.runtimes?.map((runtime) => (
          <div key={runtime.name} className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{runtime.name}</span>
              {runtime.state ? (
                <Badge
                  variant={
                    runtime.state === "current"
                      ? "secondary"
                      : runtime.state === "disabled"
                        ? "outline"
                        : "destructive"
                  }
                >
                  {t(`page.setting.xxmi.modRuntimes.state.${runtime.state}`)}
                </Badge>
              ) : null}
              <span className="text-xs text-muted-foreground">
                {runtime.version && runtime.state !== "foreign"
                  ? t("page.setting.xxmi.modRuntimes.versions", {
                      installed: runtime.version,
                      bundled: runtime.bundledVersion,
                    })
                  : t("page.setting.xxmi.modRuntimes.bundled", { bundled: runtime.bundledVersion })}
              </span>
            </div>
            <p className="text-xs text-muted-foreground">
              {t(`page.setting.xxmi.modRuntimes.usedBy.${runtime.name}`, { defaultValue: "" })}
            </p>
            {runtime.path && runtime.state !== "missing" ? (
              <p className="font-mono text-xs break-all text-muted-foreground">{runtime.path}</p>
            ) : null}
            {runtime.state === "foreign" ? (
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertDescription>
                  {t("page.setting.xxmi.modRuntimes.foreignDescription", {
                    name: runtime.name,
                    path: runtime.path,
                  })}
                </AlertDescription>
              </Alert>
            ) : null}
            {listing.modsRoot && runtime.state ? (
              <div className="flex flex-wrap gap-2">
                {runtime.state !== "current" && runtime.state !== "foreign" ? (
                  <Button
                    size="sm"
                    onClickPromise={() =>
                      run(
                        ModRuntime.Install,
                        runtime,
                        `page.setting.xxmi.modRuntimes.done.${runtime.state}`,
                      )
                    }
                  >
                    {t(`page.setting.xxmi.modRuntimes.action.${runtime.state}`)}
                  </Button>
                ) : null}
                {enabledStates.includes(runtime.state) ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onClickPromise={() =>
                      run(ModRuntime.Disable, runtime, "page.setting.xxmi.modRuntimes.disabled")
                    }
                  >
                    {t("page.setting.xxmi.modRuntimes.disable")}
                  </Button>
                ) : null}
                {runtime.state !== "missing" ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onClickPromise={async () => {
                      try {
                        await Shell.OpenPath(runtime.path);
                      } catch (error) {
                        toast.error(toErrorMessage(error));
                      }
                    }}
                  >
                    <FolderOpenIcon />
                    {t("page.setting.xxmi.modRuntimes.openFolder")}
                  </Button>
                ) : null}
                {runtime.state !== "missing" && runtime.state !== "foreign" ? (
                  <Button size="sm" variant="destructive" onClick={() => setRemoving(runtime)}>
                    {t("page.setting.xxmi.modRuntimes.remove")}
                  </Button>
                ) : null}
              </div>
            ) : null}
            {listing.modsRoot &&
            installedStates.includes(runtime.state) &&
            runtime.settings?.length ? (
              <RuntimeSettings
                runtime={runtime}
                onSave={(values) =>
                  run(
                    (target, name) => ModRuntime.SetSettings(target, name, values),
                    runtime,
                    "page.setting.xxmi.modRuntimes.settings.saved",
                  )
                }
              />
            ) : null}
          </div>
        ))}
      </SectionContent>

      <AlertDialog open={removing !== null} onOpenChange={(open) => !open && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("page.setting.xxmi.modRuntimes.removeTitle", { name: removing?.name })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.setting.xxmi.modRuntimes.removeDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (!removing) return;
                setRemoving(null);
                void run(ModRuntime.Remove, removing, "page.setting.xxmi.modRuntimes.removed");
              }}
            >
              {t("page.setting.xxmi.modRuntimes.remove")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Section>
  );
}

function RuntimeSettings({
  runtime,
  onSave,
}: {
  runtime: Status;
  onSave: (values: Record<string, string>) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const settings = runtime.settings ?? [];
  const saved = Object.fromEntries(settings.map((setting) => [setting.id, setting.value]));
  const defaults = Object.fromEntries(settings.map((setting) => [setting.id, setting.default]));
  const [draft, setDraft] = useState<Record<string, string> | null>(null);
  const values = draft ?? saved;
  const dirty = draft !== null && !isEqual(draft, saved);
  const label = (id: string) =>
    t(`page.setting.xxmi.modRuntimes.settings.${runtime.name}.${id}`, { defaultValue: id });
  const hint = (id: string) =>
    t(`page.setting.xxmi.modRuntimes.settings.${runtime.name}.hints.${id}`, { defaultValue: "" });
  const update = (id: string, value: string) => setDraft({ ...values, [id]: value });

  return (
    <div className="space-y-1 rounded-md bg-muted/50 p-3">
      <p className="text-sm font-medium">{t("page.setting.xxmi.modRuntimes.settings.title")}</p>
      <p className="text-xs text-muted-foreground">
        {runtime.state === "outdated" || runtime.state === "modified"
          ? t("page.setting.xxmi.modRuntimes.settings.repairHint")
          : t("page.setting.xxmi.modRuntimes.settings.hint")}
      </p>
      {settings.map((setting) => {
        const value = values[setting.id] ?? setting.default;
        if (setting.kind === "bool") {
          return (
            <ToggleRow
              key={setting.id}
              label={label(setting.id)}
              checked={value === "1"}
              onCheckedChange={(checked) => update(setting.id, checked ? "1" : "0")}
            />
          );
        }
        if (setting.kind === "int") {
          return (
            <NumberRow
              key={setting.id}
              label={label(setting.id)}
              description={t("page.setting.xxmi.modRuntimes.settings.range", {
                min: setting.min,
                max: setting.max,
              })}
              value={Number(value)}
              min={setting.min}
              max={setting.max}
              step={1}
              onValueChange={(next) =>
                update(
                  setting.id,
                  String(Math.min(setting.max, Math.max(setting.min, Math.round(next)))),
                )
              }
            />
          );
        }
        if (setting.kind === "number") {
          // The backend accepts plain decimals only, so values are rounded to the step's digits.
          const digits = String(setting.step).split(".")[1]?.length ?? 0;
          return (
            <NumberRow
              key={setting.id}
              label={label(setting.id)}
              description={[
                hint(setting.id),
                t("page.setting.xxmi.modRuntimes.settings.range", {
                  min: setting.min,
                  max: setting.max,
                }),
              ]
                .filter(Boolean)
                .join(" ")}
              value={Number(value)}
              min={setting.min}
              max={setting.max}
              step={setting.step || "any"}
              onValueChange={(next) =>
                update(
                  setting.id,
                  String(
                    Number(Math.min(setting.max, Math.max(setting.min, next)).toFixed(digits)),
                  ),
                )
              }
            />
          );
        }
        if (setting.kind === "color") {
          return (
            <SectionRow
              key={setting.id}
              title={label(setting.id)}
              description={hint(setting.id) || undefined}
              className="py-2"
            >
              <Input
                type="color"
                aria-label={label(setting.id)}
                className="w-16 shrink-0"
                value={colorToHex(value)}
                onChange={(event) => update(setting.id, hexToColor(event.target.value))}
              />
            </SectionRow>
          );
        }
        return (
          <SectionRow key={setting.id} title={label(setting.id)} className="py-2">
            <KeySettingDialog
              title={label(setting.id)}
              value={value}
              otherKeys={settings
                .filter((other) => other.kind === "key" && other.id !== setting.id)
                .map((other) => values[other.id] ?? other.default)}
              onSave={(next) => update(setting.id, next)}
            />
          </SectionRow>
        );
      })}
      <div className="flex flex-wrap justify-end gap-2 pt-2">
        <Button
          size="sm"
          variant="ghost"
          disabled={isEqual(values, defaults)}
          onClick={() => setDraft(defaults)}
        >
          {t("page.setting.xxmi.modRuntimes.settings.reset")}
        </Button>
        <Button
          size="sm"
          disabled={!dirty}
          onClickPromise={async () => {
            if (await onSave(values)) setDraft(null);
          }}
        >
          {t("page.setting.xxmi.modRuntimes.settings.save")}
        </Button>
      </div>
    </div>
  );
}

// A color setting is "r g b" with each channel from 0 to 1.
function colorToHex(value: string) {
  return `#${value
    .split(" ")
    .map((channel) =>
      Math.round(Math.min(1, Math.max(0, Number(channel) || 0)) * 255)
        .toString(16)
        .padStart(2, "0"),
    )
    .join("")}`;
}

function hexToColor(hex: string) {
  return [1, 3, 5]
    .map((start) => String(Number((parseInt(hex.slice(start, start + 2), 16) / 255).toFixed(3))))
    .join(" ");
}

function KeySettingDialog({
  title,
  value,
  otherKeys,
  onSave,
}: {
  title: string;
  value: string;
  otherKeys: string[];
  onSave: (value: string) => void;
}) {
  const { t } = useTranslation();
  const labels = value
    .split(" ")
    .map((key) => formatKeyLabel(key))
    .filter((key): key is string => key !== null);

  return (
    <Dialog>
      <DialogTrigger render={<Button variant="outline" size="sm" />}>
        {labels.map((key, index) => (
          <Kbd key={`${key}-${index}`}>{key}</Kbd>
        ))}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {t("page.setting.xxmi.modRuntimes.settings.keyDescription")}
          </DialogDescription>
        </DialogHeader>
        <KeyRecorder defaultValue={value} otherKeys={otherKeys} onSave={onSave} />
      </DialogContent>
    </Dialog>
  );
}
