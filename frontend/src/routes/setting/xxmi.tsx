import { Dialog } from "@bindings/platform";
import { XXMI } from "@bindings/xxmi";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import { Separator } from "@renderer/components/ui/separator";
import { Switch } from "@renderer/components/ui/switch";
import { useLaunchGuard } from "@renderer/hooks/use-launch-guard";
import { useSettings } from "@renderer/hooks/use-settings";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/xxmi")({ component: RouteComponent });

export type XXMIData = Awaited<ReturnType<typeof XXMI.GetXXMIData>>;

const importerKeys = ["GIMI", "SRMI", "HIMI", "ZZMI", "WWMI", "EFMI"] as const;
const settingsConfig = {
  autoUpdate: "xxmi.autoUpdate",
  includePrereleases: "xxmi.includePrereleases",
} as const;

function RouteComponent() {
  const location = useLocation();
  return location.pathname.startsWith("/setting/xxmi/") ? <Outlet /> : <XXMIDashboard />;
}

function XXMIDashboard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { settings, update } = useSettings(settingsConfig);
  const { startImporter, launchGuardDialog } = useLaunchGuard();
  const { data: overview } = useQuery({ queryKey: ["xxmi:overview"], queryFn: XXMI.GetOverview });
  const { data: libs } = useQuery({ queryKey: ["xxmi:libs-cache"], queryFn: XXMI.ListCachedLibs });
  const { data: legacy } = useQuery({
    queryKey: ["xxmi:legacy-cache"],
    queryFn: XXMI.GetLegacyRuntimes,
  });
  const { data: updates } = useQuery({
    queryKey: ["xxmi:updates"],
    queryFn: () => XXMI.CheckUpdates(false),
    enabled: overview?.configured ?? false,
    staleTime: 60 * 60 * 1000,
  });
  const [editedRoot, setEditedRoot] = useState<string | null>(null);
  const root = editedRoot ?? overview?.root ?? "";

  const refresh = () => {
    void queryClient.invalidateQueries({
      predicate: (query) =>
        typeof query.queryKey[0] === "string" && query.queryKey[0].startsWith("xxmi:"),
    });
  };

  return (
    <main className="mx-auto flex w-full flex-1 flex-col space-y-6 p-4 select-none">
      <Card>
        <CardHeader>
          <CardTitle>{t("page.setting.xxmi.builtin.root")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex gap-2">
            <Input value={root} onChange={(event) => setEditedRoot(event.target.value)} />
            <Button
              disabled={!root || root === overview?.root}
              onClickPromise={async () => {
                try {
                  await XXMI.SetRoot(root);
                  setEditedRoot(null);
                  refresh();
                  toast.success(t("page.setting.xxmi.builtin.saved"));
                } catch (error) {
                  toast.error(toErrorMessage(error));
                }
              }}
            >
              {t("g.save")}
            </Button>
          </div>
          {overview?.externalLauncher && (
            <div className="space-y-2 rounded-md border p-3 text-sm">
              <p>{t("page.setting.xxmi.builtin.externalDetected")}</p>
              <p className="break-all text-muted-foreground">{overview.externalLauncher.path}</p>
              <p className="text-muted-foreground">
                {t("page.setting.xxmi.builtin.externalWarning")}
              </p>
              <Button
                variant="outline"
                onClickPromise={async () => {
                  try {
                    await XXMI.ImportExternalLauncher({
                      path: overview.externalLauncher!.path,
                      root: root || overview.root,
                    });
                    refresh();
                    toast.success(t("page.setting.xxmi.builtin.imported"));
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  }
                }}
              >
                {t("page.setting.xxmi.builtin.import")}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("page.setting.xxmi.builtin.packages")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <div className="flex items-center justify-between gap-3">
            <span>
              {t("page.setting.xxmi.builtin.libs")}: {libs?.length ?? 0}
            </span>
            <Button
              variant="outline"
              onClickPromise={async () => {
                try {
                  const removed = await XXMI.PruneLibsCache();
                  refresh();
                  toast.success(
                    t("page.setting.xxmi.builtin.pruned", { count: removed?.length ?? 0 }),
                  );
                } catch (error) {
                  toast.error(toErrorMessage(error));
                }
              }}
            >
              {t("page.setting.xxmi.builtin.prune")}
            </Button>
          </div>
          {libs?.map((entry) => (
            <p key={entry.version} className="text-muted-foreground">
              {entry.version} {entry.referenced && `· ${t("page.setting.xxmi.builtin.inUse")}`}
            </p>
          ))}
          <Separator />
          <div className="flex items-center justify-between gap-3">
            <span>
              {t("page.setting.xxmi.builtin.legacy")}: {legacy?.length ?? 0}
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                onClickPromise={async () => {
                  try {
                    const selected = await Dialog.ShowOpenDialog({
                      title: t("page.setting.xxmi.builtin.importLegacy"),
                      defaultPath: "",
                      filters: [{ name: "ZIP", extensions: ["zip"] }],
                      properties: ["openFile"],
                    });
                    if (selected.canceled || !selected.filePaths?.[0]) return;
                    await XXMI.ImportLegacyRuntimeZip(selected.filePaths[0]);
                    refresh();
                    toast.success(t("page.setting.xxmi.builtin.importedLegacy"));
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  }
                }}
              >
                {t("page.setting.xxmi.builtin.importLegacy")}
              </Button>
              <Button
                variant="outline"
                onClickPromise={async () => {
                  try {
                    await XXMI.UpdateLegacyRuntime();
                    refresh();
                    toast.success(t("page.setting.xxmi.builtin.downloaded"));
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  }
                }}
              >
                {t("page.setting.xxmi.builtin.downloadLegacy")}
              </Button>
            </div>
          </div>
          {legacy?.map((entry) => (
            <p key={entry.id} className="text-muted-foreground">
              {entry.id}
            </p>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("page.setting.xxmi.builtin.options")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between gap-3 text-sm">
            <span>{t("page.setting.xxmi.builtin.autoUpdate")}</span>
            <Switch
              checked={settings?.autoUpdate ?? false}
              onCheckedChange={(value) => update("autoUpdate", value)}
            />
          </div>
          <div className="flex items-center justify-between gap-3 text-sm">
            <span>{t("page.setting.xxmi.builtin.prereleases")}</span>
            <Switch
              checked={settings?.includePrereleases ?? false}
              onCheckedChange={(value) => update("includePrereleases", value)}
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-3">
            <CardTitle>{t("page.setting.xxmi.builtin.importers")}</CardTitle>
            <div className="flex gap-2">
              <Button
                variant="outline"
                onClickPromise={async () => {
                  try {
                    await XXMI.CheckUpdates(true);
                    refresh();
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  }
                }}
              >
                {t("page.setting.xxmi.builtin.checkUpdates")}
              </Button>
              <Button
                variant="outline"
                disabled={!updates?.some((entry) => entry.available && !entry.pinned)}
                onClickPromise={async () => {
                  try {
                    const targets = [
                      ...new Set(
                        updates
                          ?.filter((entry) => entry.available && !entry.pinned)
                          .map((entry) => entry.package) ?? [],
                      ),
                    ];
                    await XXMI.InstallUpdates(targets);
                    refresh();
                    toast.success(t("page.setting.xxmi.builtin.updatesInstalled"));
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  }
                }}
              >
                {t("page.setting.xxmi.builtin.installUpdates")}
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          {updates
            ?.filter(
              (entry, index, list) =>
                entry.available &&
                !entry.pinned &&
                list.findIndex((item) => item.package === entry.package) === index,
            )
            .map((entry) => (
              <div
                key={entry.package}
                className="flex items-center justify-between gap-3 rounded-md border p-3 text-sm"
              >
                <span className="min-w-0 break-all">
                  {entry.package}: {entry.installed || t("page.setting.xxmi.builtin.notInstalled")}{" "}
                  → {entry.latestVersion}
                </span>
                <Button
                  variant="outline"
                  onClickPromise={async () => {
                    try {
                      await XXMI.SkipVersion(entry.package, entry.latestVersion);
                      refresh();
                      toast.success(t("page.setting.xxmi.builtin.versionSkipped"));
                    } catch (error) {
                      toast.error(toErrorMessage(error));
                    }
                  }}
                >
                  {t("page.setting.xxmi.builtin.skipVersion")}
                </Button>
              </div>
            ))}
          {importerKeys.map((key) => {
            const importer = overview?.importers?.find((entry) => entry.key === key);
            const available = updates?.some((entry) => entry.importer === key && entry.available);
            return (
              <div
                key={key}
                className="flex items-center justify-between gap-3 rounded-md border p-3"
              >
                <div className="min-w-0 text-sm">
                  <p className="font-medium">
                    {key} {importer && `· ${importer.mode === "legacy" ? "3DMigoto" : "XXMI"}`}
                    {available && ` · ${t("page.setting.xxmi.builtin.updateAvailable")}`}
                  </p>
                  <p className="truncate text-muted-foreground">
                    {importer?.packageInfo.deployed_version ||
                      t("page.setting.xxmi.builtin.notInstalled")}
                  </p>
                </div>
                <div className="flex gap-2">
                  {importer && (
                    <Button
                      variant="outline"
                      onClickPromise={async () => {
                        try {
                          await startImporter(key);
                        } catch (error) {
                          toast.error(toErrorMessage(error));
                        }
                      }}
                    >
                      {t("page.setting.xxmi.builtin.launch")}
                    </Button>
                  )}
                  <Button
                    variant="outline"
                    onClick={() =>
                      navigate({
                        to: "/setting/xxmi/$importer",
                        params: { importer: key },
                      })
                    }
                  >
                    {t("page.setting.xxmi.builtin.configure")}
                  </Button>
                </div>
              </div>
            );
          })}
        </CardContent>
      </Card>
      {launchGuardDialog}
    </main>
  );
}
