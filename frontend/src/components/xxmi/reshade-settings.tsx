import { Shell } from "@bindings/platform";
import { ReShade, type EffectPackage, type Paths } from "@bindings/reshade";
import { XXMI } from "@bindings/xxmi";
import { LauncherMode, RuntimeMode } from "@bindings/xxmi/models";
import { GameIcon } from "@renderer/components/game-icon";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import {
  Section,
  SectionAction,
  SectionContent,
  SectionDescription,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import { Switch } from "@renderer/components/ui/switch";
import { FOLLOW_LATEST, SelectRow } from "@renderer/components/xxmi/xxmi-fields";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate } from "@tanstack/react-router";
import { DownloadIcon, FolderOpenIcon, Loader2Icon, Trash2Icon } from "lucide-react";
import { useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const sharedFolders = [
  "shaders",
  "textures",
  "addons",
  "presets",
] as const satisfies (keyof Paths)[];

export function ReShadeSettings() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const versionChange = useRef(Promise.resolve());
  const { data: overview } = useQuery({ queryKey: ["xxmi:overview"], queryFn: XXMI.GetOverview });
  const { data: status } = useQuery({ queryKey: ["reshade:status"], queryFn: ReShade.Status });
  const { data: versions } = useQuery({
    queryKey: ["reshade:versions"],
    queryFn: () => ReShade.Versions(false),
    staleTime: 60 * 60 * 1000,
    retry: false,
  });
  const target = status?.pinned || status?.latest;
  const importers = overview?.importers ?? [];

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["reshade:status"] });
  };

  // Only the built-in launcher injects ReShade, and the external one hides the way back to this page.
  if (overview?.launcherMode === LauncherMode.LauncherExternal) return <Navigate to="/xxmi" />;

  return (
    <main className="min-h-0 flex-1 scrollbar-gutter-stable overflow-y-auto">
      <div className="mx-auto flex w-full max-w-2xl flex-col gap-6 p-4">
        <Section>
          <SectionHeader>
            <SectionTitle>{t("page.setting.xxmi.builtin.reshade.title")}</SectionTitle>
            <SectionDescription>
              {t("page.setting.xxmi.builtin.reshade.description")}
            </SectionDescription>
          </SectionHeader>
          <SectionContent>
            <SectionRow
              title={t("page.setting.xxmi.builtin.reshade.binary")}
              description={
                <>
                  {status?.active
                    ? t("page.setting.xxmi.builtin.reshade.installed", { version: status.active })
                    : t("page.setting.xxmi.builtin.reshade.notInstalled")}
                  {status?.updateAvailable && status.active && (
                    <span className="mt-0.5 block">
                      {t("page.setting.xxmi.builtin.reshade.updateAvailable", {
                        version: status.latest,
                      })}
                    </span>
                  )}
                </>
              }
            >
              <Button
                variant="outline"
                size="sm"
                className="shrink-0"
                disabled={!target || status?.active === target}
                onClickPromise={async () => {
                  try {
                    await ReShade.Install();
                    refresh();
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  }
                }}
              >
                <DownloadIcon />
                {status?.active
                  ? t("page.setting.xxmi.builtin.reshade.update")
                  : t("page.setting.xxmi.builtin.reshade.download")}
              </Button>
            </SectionRow>
            <SelectRow
              label={t("page.setting.xxmi.builtin.reshade.version")}
              description={t("page.setting.xxmi.builtin.reshade.versionDescription")}
              value={status?.pinned || FOLLOW_LATEST}
              options={[
                { value: FOLLOW_LATEST, label: t("page.setting.xxmi.builtin.latest") },
                ...(versions ?? (status?.pinned ? [status.pinned] : [])),
              ]}
              onValueChange={(value) => {
                // A version is downloaded before it is stored, so an earlier choice could finish last.
                versionChange.current = versionChange.current
                  .then(() => ReShade.SetVersion(value === FOLLOW_LATEST ? "" : value))
                  .then(refresh, (error: unknown) => {
                    toast.error(toErrorMessage(error));
                  });
              }}
            />
          </SectionContent>
        </Section>

        <Section>
          <SectionHeader>
            <SectionTitle>{t("page.setting.xxmi.builtin.reshade.games")}</SectionTitle>
            <SectionDescription>
              {t("page.setting.xxmi.builtin.reshade.toggleDescription")}{" "}
              {t("page.setting.xxmi.builtin.reshade.gameFolderDescription")}
            </SectionDescription>
          </SectionHeader>
          <SectionContent>
            {importers.map((importer) => (
              <GameRow key={importer.key} importer={importer.key} />
            ))}
            {overview && importers.length === 0 && (
              <p className="text-xs text-muted-foreground">
                {t("page.setting.xxmi.builtin.reshade.gamesEmpty")}
              </p>
            )}
          </SectionContent>
        </Section>

        <EffectPackages />

        <Section>
          <SectionHeader>
            <SectionTitle>{t("page.setting.xxmi.builtin.reshade.folders")}</SectionTitle>
            <SectionDescription>
              {t("page.setting.xxmi.builtin.reshade.foldersDescription")}
            </SectionDescription>
          </SectionHeader>
          <SectionContent>
            <div className="flex flex-wrap gap-2">
              {sharedFolders.map((folder) => (
                <Button
                  key={folder}
                  variant="outline"
                  size="sm"
                  onClickPromise={async () => {
                    try {
                      const paths = await ReShade.Paths("");
                      await Shell.OpenPath(paths[folder]);
                    } catch (error) {
                      toast.error(toErrorMessage(error));
                    }
                  }}
                >
                  <FolderOpenIcon />
                  {t(`page.setting.xxmi.builtin.reshade.folderNames.${folder}`)}
                </Button>
              ))}
            </div>
          </SectionContent>
        </Section>
      </div>
    </main>
  );
}

function GameRow({ importer }: { importer: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const labelId = useId();
  const { data: config } = useQuery({
    queryKey: ["xxmi:config", importer],
    queryFn: () => XXMI.GetImporterConfig(importer),
  });
  // Shown until the saved config is read back, so the switch does not flip twice.
  const [pending, setPending] = useState<boolean | null>(null);
  const enabled = pending ?? config?.reshade.enabled ?? false;
  // The 3DMigoto loader injects on its own schedule, so ReShade cannot be loaded ahead of it.
  const unsupported =
    !!config && config.mode !== RuntimeMode.RuntimeXXMI && config.injectionMethod !== "Native";

  const setEnabled = async (next: boolean) => {
    setPending(next);
    try {
      // The config is read again so a change saved on the importer's page is not written back stale.
      const current = await XXMI.GetImporterConfig(importer);
      await XXMI.SaveImporterConfig(importer, { ...current, reshade: { enabled: next } });
    } catch (error) {
      toast.error(`${importer}: ${toErrorMessage(error)}`);
    }
    await queryClient.invalidateQueries({ queryKey: ["xxmi:config", importer] });
    setPending(null);
  };

  return (
    <div className="flex items-center gap-3">
      <GameIcon gameName={importer} className="size-8 shrink-0 rounded-md" />
      <div className="min-w-0 flex-1 space-y-0.5">
        <span id={labelId} className="text-sm font-medium">
          {importer}
        </span>
        {unsupported && (
          <p className="text-xs text-muted-foreground">
            {t("page.setting.xxmi.builtin.reshade.legacyUnsupported")}
          </p>
        )}
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={`${importer}: ${t("page.setting.xxmi.builtin.reshade.gameFolder")}`}
        title={t("page.setting.xxmi.builtin.reshade.gameFolder")}
        onClickPromise={async () => {
          try {
            const paths = await ReShade.Paths(importer);
            await Shell.OpenPath(paths.game);
          } catch (error) {
            toast.error(toErrorMessage(error));
          }
        }}
      >
        <FolderOpenIcon />
      </Button>
      <Switch
        aria-labelledby={labelId}
        checked={enabled}
        disabled={!config || pending !== null || (unsupported && !enabled)}
        onCheckedChange={(next) => void setEnabled(next)}
      />
    </div>
  );
}

function EffectPackages() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const query = useQuery({
    queryKey: ["reshade:effects"],
    queryFn: ReShade.ListEffectPackages,
    retry: false,
  });
  const packages = query.data ?? [];

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await action();
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
    // A failed batch still installs the packages before the failure.
    await queryClient.invalidateQueries({ queryKey: ["reshade:effects"] });
    setSelected([]);
    setBusy(false);
  };

  return (
    <Section>
      <SectionHeader>
        <SectionTitle>{t("page.setting.xxmi.builtin.reshade.effects")}</SectionTitle>
        <SectionDescription>
          {t("page.setting.xxmi.builtin.reshade.effectsDescription")}
        </SectionDescription>
        <SectionAction className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={busy || packages.length === 0}
            onClick={() =>
              setSelected(
                packages
                  .filter((pkg) => pkg.recommended && pkg.supported && !pkg.installed)
                  .map((pkg) => pkg.id),
              )
            }
          >
            {t("page.setting.xxmi.builtin.reshade.selectRecommended")}
          </Button>
          <Button
            size="sm"
            disabled={busy || selected.length === 0}
            onClickPromise={() => run(() => ReShade.InstallEffectPackages(selected))}
          >
            <DownloadIcon />
            {t("page.setting.xxmi.builtin.reshade.installSelected", { count: selected.length })}
          </Button>
        </SectionAction>
      </SectionHeader>
      <SectionContent>
        {query.isPending ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2Icon className="size-3.5 animate-spin" />
            {t("page.setting.xxmi.builtin.reshade.effectsLoading")}
          </div>
        ) : query.isError ? (
          <div className="flex items-center justify-between gap-4">
            <p className="text-sm text-destructive">
              {t("page.setting.xxmi.builtin.reshade.effectsLoadFailed")}
            </p>
            <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
              {t("page.setting.xxmi.dllVersionRetry")}
            </Button>
          </div>
        ) : (
          <ul className="-mx-2 max-h-96 space-y-1 overflow-y-auto">
            {packages.map((pkg) => (
              <EffectPackageItem
                key={pkg.id}
                pkg={pkg}
                busy={busy}
                checked={selected.includes(pkg.id)}
                onCheckedChange={(checked) =>
                  setSelected(
                    checked ? [...selected, pkg.id] : selected.filter((id) => id !== pkg.id),
                  )
                }
                onRemove={() => run(() => ReShade.RemoveEffectPackage(pkg.id))}
              />
            ))}
          </ul>
        )}
      </SectionContent>
    </Section>
  );
}

function EffectPackageItem({
  pkg,
  busy,
  checked,
  onCheckedChange,
  onRemove,
}: {
  pkg: EffectPackage;
  busy: boolean;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  onRemove: () => Promise<void>;
}) {
  const { t } = useTranslation();

  return (
    <li className="flex items-start gap-3 rounded-md p-2 hover:bg-muted/50">
      <label className="flex min-w-0 flex-1 items-start gap-3">
        <Checkbox
          className="mt-0.5"
          checked={checked}
          disabled={busy || !pkg.supported}
          onCheckedChange={(value) => onCheckedChange(value === true)}
        />
        <span className="min-w-0 flex-1 space-y-0.5">
          <span className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
            {pkg.name}
            {pkg.installed && (
              <Badge variant="secondary">
                {t("page.setting.xxmi.builtin.reshade.effectInstalled")}
              </Badge>
            )}
            {!pkg.supported && (
              <Badge variant="outline">
                {t("page.setting.xxmi.builtin.reshade.effectUnsupported")}
              </Badge>
            )}
          </span>
          {pkg.description && (
            <span className="block text-xs text-muted-foreground">{pkg.description}</span>
          )}
        </span>
      </label>
      {pkg.installed && (
        <Button
          variant="ghost"
          size="icon"
          className="shrink-0"
          disabled={busy}
          aria-label={t("page.setting.xxmi.builtin.reshade.removeEffect", { name: pkg.name })}
          title={t("page.setting.xxmi.builtin.reshade.removeEffect", { name: pkg.name })}
          onClickPromise={onRemove}
        >
          <Trash2Icon />
        </Button>
      )}
    </li>
  );
}
