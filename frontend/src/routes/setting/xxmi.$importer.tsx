import { XXMI } from "@bindings/xxmi";
import { RuntimeMode, type ImporterConfig } from "@bindings/xxmi/models";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Switch } from "@renderer/components/ui/switch";
import { useLaunchGuard } from "@renderer/hooks/use-launch-guard";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/xxmi/$importer")({ component: RouteComponent });

function RouteComponent() {
  const { importer } = Route.useParams();
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { startImporter, launchGuardDialog } = useLaunchGuard();
  const { data: saved } = useQuery({
    queryKey: ["xxmi:config", importer],
    queryFn: () => XXMI.GetImporterConfig(importer),
  });
  const { data: releases } = useQuery({
    queryKey: ["xxmi:releases", importer],
    queryFn: () => XXMI.ListReleases(`importer:${importer}`),
  });
  const { data: libsReleases } = useQuery({
    queryKey: ["xxmi:libs-releases"],
    queryFn: () => XXMI.ListReleases("xxmi-libs"),
  });
  const [draft, setConfig] = useState<ImporterConfig | null>(null);
  const config = draft ?? saved ?? null;
  const [selectedPackage, setSelectedPackage] = useState("");
  const [allowUnsigned, setAllowUnsigned] = useState(false);
  const [optimizationPreview, setOptimizationPreview] = useState<
    Awaited<ReturnType<typeof XXMI.OptimizeMods>> | undefined
  >();
  const [detectedFolders, setDetectedFolders] = useState<
    Awaited<ReturnType<typeof XXMI.DetectGameFolders>> | undefined
  >();

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["xxmi:config", importer] });
    void queryClient.invalidateQueries({ queryKey: ["xxmi:overview"] });
    void queryClient.invalidateQueries({ queryKey: ["xxmi:updates"] });
  };
  const save = async (next = config) => {
    if (!next) return;
    try {
      if (next.gameFolder) await XXMI.ValidateGameFolder(importer, next.gameFolder);
      if (next.xxmiVersion.pinned !== saved?.xxmiVersion.pinned) {
        await XXMI.SetImporterVersions(importer, { xxmi: next.xxmiVersion });
      }
      await XXMI.SaveImporterConfig(importer, next);
      setConfig(null);
      refresh();
      toast.success(t("page.setting.xxmi.builtin.saved"));
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  };

  if (!config) return null;

  return (
    <main className="mx-auto flex w-full flex-1 flex-col space-y-6 p-4 select-none">
      <div className="flex items-center justify-between">
        <Button variant="outline" onClick={() => navigate({ to: "/setting/xxmi" })}>
          {t("page.setting.xxmi.builtin.back")}
        </Button>
        <Button onClickPromise={() => save()}>{t("g.save")}</Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>
            {importer} · {t("page.setting.xxmi.builtin.general")}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <div className="flex items-center justify-between">
            <span>{t("page.setting.xxmi.builtin.enabled")}</span>
            <Switch
              checked={config.enabled}
              onCheckedChange={(enabled) => setConfig({ ...config, enabled })}
            />
          </div>
          <label className="block space-y-1">
            <span>{t("page.setting.xxmi.builtin.importerFolder")}</span>
            <Input
              value={config.importerFolder}
              onChange={(event) => setConfig({ ...config, importerFolder: event.target.value })}
            />
          </label>
          <div className="space-y-1">
            <span>{t("page.setting.xxmi.builtin.gameFolder")}</span>
            <div className="flex gap-2">
              <Input
                value={config.gameFolder}
                onChange={(event) => setConfig({ ...config, gameFolder: event.target.value })}
              />
              <Button
                variant="outline"
                onClickPromise={async () => {
                  try {
                    setDetectedFolders(await XXMI.DetectGameFolders(importer));
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  }
                }}
              >
                {t("page.setting.xxmi.builtin.detectGame")}
              </Button>
            </div>
          </div>
          <div className="flex items-center gap-3">
            <span>{t("page.setting.xxmi.builtin.mode")}</span>
            <Button
              variant={config.mode === RuntimeMode.RuntimeXXMI ? "default" : "outline"}
              onClick={() => setConfig({ ...config, mode: RuntimeMode.RuntimeXXMI })}
            >
              XXMI
            </Button>
            <Button
              variant={config.mode === RuntimeMode.RuntimeLegacy ? "default" : "outline"}
              onClick={() => setConfig({ ...config, mode: RuntimeMode.RuntimeLegacy })}
            >
              3DMigoto
            </Button>
          </div>
          {config.mode === RuntimeMode.RuntimeLegacy && (
            <p className="text-muted-foreground">{t("page.setting.xxmi.builtin.legacyWarning")}</p>
          )}
          <Button
            variant="outline"
            onClickPromise={async () => {
              try {
                await startImporter(importer);
              } catch (error) {
                toast.error(toErrorMessage(error));
              }
            }}
          >
            {t("page.setting.xxmi.builtin.launch")}
          </Button>
        </CardContent>
      </Card>

      <Dialog
        open={detectedFolders !== undefined}
        onOpenChange={(open) => !open && setDetectedFolders(undefined)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("page.setting.xxmi.builtin.detectGame")}</DialogTitle>
          </DialogHeader>
          {detectedFolders?.length ? (
            <div className="max-h-80 space-y-2 overflow-y-auto">
              {detectedFolders.map((candidate) => (
                <Button
                  key={candidate.exePath}
                  variant="outline"
                  className="h-auto w-full justify-start text-left break-all whitespace-normal"
                  onClick={() => {
                    setConfig({ ...config, gameFolder: candidate.path });
                    setDetectedFolders(undefined);
                  }}
                >
                  {candidate.path}
                </Button>
              ))}
            </div>
          ) : (
            <p className="text-muted-foreground">{t("page.setting.xxmi.builtin.noGameFolders")}</p>
          )}
        </DialogContent>
      </Dialog>

      <Card>
        <CardHeader>
          <CardTitle>{t("page.setting.xxmi.builtin.packageVersion")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <p>
            {t("page.setting.xxmi.builtin.currentPin")}:{" "}
            {config.packageVersion.pinned || t("page.setting.xxmi.builtin.latest")}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button
              variant={!config.packageVersion.pinned ? "default" : "outline"}
              onClick={() => setConfig({ ...config, packageVersion: { follow: "latest" } })}
            >
              {t("page.setting.xxmi.builtin.latest")}
            </Button>
            {releases?.map((release) => (
              <Button
                key={release.version}
                variant={config.packageVersion.pinned === release.version ? "default" : "outline"}
                onClick={() => {
                  setSelectedPackage(release.version);
                  setConfig({
                    ...config,
                    packageVersion: { pinned: release.version },
                  });
                }}
              >
                {release.version} {release.signed ? "✓" : "!"}
              </Button>
            ))}
          </div>
          {selectedPackage && (
            <>
              <label className="flex items-center gap-2">
                <Switch checked={allowUnsigned} onCheckedChange={setAllowUnsigned} />
                {t("page.setting.xxmi.builtin.allowUnsigned")}
              </label>
              <Button
                onClickPromise={async () => {
                  try {
                    if (config.gameFolder) {
                      await XXMI.ValidateGameFolder(importer, config.gameFolder);
                    }
                    await XXMI.SaveImporterConfig(importer, config);
                    await XXMI.InstallImporterPackage({
                      importer,
                      version: selectedPackage,
                      allowUnsigned,
                    });
                    refresh();
                    toast.success(t("page.setting.xxmi.builtin.installed"));
                  } catch (error) {
                    toast.error(toErrorMessage(error));
                  }
                }}
              >
                {t("page.setting.xxmi.builtin.install")}
              </Button>
            </>
          )}
          <p>
            {t("page.setting.xxmi.builtin.libs")}:{" "}
            {config.xxmiVersion.pinned || t("page.setting.xxmi.builtin.latest")}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button
              variant={!config.xxmiVersion.pinned ? "default" : "outline"}
              onClick={() => setConfig({ ...config, xxmiVersion: { follow: "latest" } })}
            >
              {t("page.setting.xxmi.builtin.latest")}
            </Button>
            {libsReleases?.map((release) => (
              <Button
                key={release.version}
                variant={config.xxmiVersion.pinned === release.version ? "default" : "outline"}
                onClick={() => setConfig({ ...config, xxmiVersion: { pinned: release.version } })}
              >
                {release.version}
              </Button>
            ))}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("page.setting.xxmi.builtin.launchOptions")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <label className="flex items-center justify-between">
            {t("page.setting.xxmi.builtin.useLaunchOptions")}
            <Switch
              checked={config.useLaunchOptions}
              onCheckedChange={(useLaunchOptions) => setConfig({ ...config, useLaunchOptions })}
            />
          </label>
          <Input
            value={config.launchOptions}
            onChange={(event) => setConfig({ ...config, launchOptions: event.target.value })}
          />
          <label className="block space-y-1">
            <span>{t("page.setting.xxmi.builtin.timeout")}</span>
            <Input
              type="number"
              value={config.processTimeout}
              onChange={(event) =>
                setConfig({ ...config, processTimeout: Number(event.target.value) })
              }
            />
          </label>
          <div className="space-y-2">
            <span>{t("page.setting.xxmi.builtin.startMethod")}</span>
            <div className="flex flex-wrap gap-2">
              {["Native", "Shell", "Manual"].map((method) => (
                <Button
                  key={method}
                  variant={config.processStartMethod === method ? "default" : "outline"}
                  onClick={() => setConfig({ ...config, processStartMethod: method })}
                >
                  {method}
                </Button>
              ))}
            </div>
          </div>
          <div className="space-y-2">
            <span>{t("page.setting.xxmi.builtin.priority")}</span>
            <div className="flex flex-wrap gap-2">
              {["Low", "BelowNormal", "Normal", "AboveNormal", "High", "Realtime"].map(
                (priority) => (
                  <Button
                    key={priority}
                    variant={config.processPriority === priority ? "default" : "outline"}
                    onClick={() => setConfig({ ...config, processPriority: priority })}
                  >
                    {priority}
                  </Button>
                ),
              )}
            </div>
          </div>
          <label className="block space-y-1">
            <span>{t("page.setting.xxmi.builtin.initDelay")}</span>
            <Input
              type="number"
              value={config.xxmiDLLInitDelay}
              onChange={(event) =>
                setConfig({ ...config, xxmiDLLInitDelay: Number(event.target.value) })
              }
            />
          </label>
          <div className="space-y-2">
            <span>{t("page.setting.xxmi.builtin.windowMode")}</span>
            <div className="flex flex-wrap gap-2">
              {["Windowed", "Borderless", "Fullscreen", "Exclusive Fullscreen"].map((mode) => (
                <Button
                  key={mode}
                  variant={config.windowMode === mode ? "default" : "outline"}
                  onClick={() => setConfig({ ...config, windowMode: mode })}
                >
                  {mode}
                </Button>
              ))}
            </div>
          </div>
        </CardContent>
      </Card>

      {(config.gimi || config.srmi || config.himi || config.wwmi) && (
        <Card>
          <CardHeader>
            <CardTitle>{t("page.setting.xxmi.builtin.gameTweaks")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <label className="flex items-center justify-between">
              <span>{t("page.setting.xxmi.builtin.configureGame")}</span>
              <Switch
                checked={config.configureGame}
                onCheckedChange={(configureGame) => setConfig({ ...config, configureGame })}
              />
            </label>
            {config.gimi && (
              <>
                <label className="flex items-center justify-between">
                  <span>{t("page.setting.xxmi.builtin.unlockFPS")}</span>
                  <Switch
                    checked={config.gimi.unlockFPS}
                    onCheckedChange={(unlockFPS) =>
                      setConfig({ ...config, gimi: { ...config.gimi!, unlockFPS } })
                    }
                  />
                </label>
                {config.gimi.unlockFPS && (
                  <label className="block space-y-1">
                    <span>{t("page.setting.xxmi.builtin.fpsTarget")}</span>
                    <Input
                      type="number"
                      min={30}
                      max={1000}
                      value={config.gimi.unlockFPSValue}
                      onChange={(event) =>
                        setConfig({
                          ...config,
                          gimi: { ...config.gimi!, unlockFPSValue: Number(event.target.value) },
                        })
                      }
                    />
                  </label>
                )}
                {(
                  [
                    ["enableHDR", "enableHDR"],
                    ["disableDCR", "disableDCR"],
                  ] as const
                ).map(([field, label]) => (
                  <label key={field} className="flex items-center justify-between">
                    <span>{t(`page.setting.xxmi.builtin.${label}`)}</span>
                    <Switch
                      checked={config.gimi![field]}
                      onCheckedChange={(value) =>
                        setConfig({ ...config, gimi: { ...config.gimi!, [field]: value } })
                      }
                    />
                  </label>
                ))}
              </>
            )}
            {config.srmi && (
              <label className="flex items-center justify-between">
                <span>{t("page.setting.xxmi.builtin.unlockFPS")}</span>
                <Switch
                  checked={config.srmi.unlockFPS}
                  onCheckedChange={(unlockFPS) =>
                    setConfig({ ...config, srmi: { ...config.srmi!, unlockFPS } })
                  }
                />
              </label>
            )}
            {config.himi && (
              <>
                <label className="flex items-center justify-between">
                  <span>{t("page.setting.xxmi.builtin.unlockFPS")}</span>
                  <Switch
                    checked={config.himi.unlockFPS}
                    onCheckedChange={(unlockFPS) =>
                      setConfig({ ...config, himi: { ...config.himi!, unlockFPS } })
                    }
                  />
                </label>
                {config.himi.unlockFPS && (
                  <label className="block space-y-1">
                    <span>{t("page.setting.xxmi.builtin.fpsTarget")}</span>
                    <Input
                      type="number"
                      min={30}
                      max={1000}
                      value={config.himi.unlockFPSValue}
                      onChange={(event) =>
                        setConfig({
                          ...config,
                          himi: { ...config.himi!, unlockFPSValue: Number(event.target.value) },
                        })
                      }
                    />
                  </label>
                )}
              </>
            )}
            {config.wwmi && (
              <>
                {(
                  [
                    ["unlockFPS", "unlockFPS"],
                    ["forceMaxLODBias", "forceMaxLODBias"],
                    ["applyPerfTweaks", "applyPerfTweaks"],
                    ["disableWoundedFX", "disableWoundedFX"],
                  ] as const
                ).map(([field, label]) => (
                  <label key={field} className="flex items-center justify-between">
                    <span>{t(`page.setting.xxmi.builtin.${label}`)}</span>
                    <Switch
                      checked={config.wwmi![field]}
                      onCheckedChange={(value) =>
                        setConfig({
                          ...config,
                          woundedFXDecided:
                            field === "disableWoundedFX" ? true : config.woundedFXDecided,
                          wwmi: { ...config.wwmi!, [field]: value },
                        })
                      }
                    />
                  </label>
                ))}
              </>
            )}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>3DMigoto</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          {(
            [
              ["enforceRendering", "enforceRendering"],
              ["enableHunting", "enableHunting"],
              ["dumpShaders", "dumpShaders"],
              ["muteWarnings", "muteWarnings"],
              ["callsLogging", "callsLogging"],
              ["debugLogging", "debugLogging"],
              ["unsafeMode", "unsafeMode"],
            ] as const
          ).map(([field, label]) => (
            <label key={field} className="flex items-center justify-between">
              <span>{t(`page.setting.xxmi.builtin.${label}`)}</span>
              <Switch
                checked={config.migoto[field]}
                onCheckedChange={(value) =>
                  setConfig({ ...config, migoto: { ...config.migoto, [field]: value } })
                }
              />
            </label>
          ))}
          {config.migoto.unsafeMode && (
            <p className="text-muted-foreground">{t("page.setting.xxmi.builtin.unsafeWarning")}</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("page.setting.xxmi.builtin.advanced")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <label className="flex items-center justify-between">
            <span>{t("page.setting.xxmi.builtin.preLaunch")}</span>
            <Switch
              checked={config.runPreLaunch.enabled}
              onCheckedChange={(enabled) =>
                setConfig({ ...config, runPreLaunch: { ...config.runPreLaunch, enabled } })
              }
            />
          </label>
          <Input
            value={config.runPreLaunch.command}
            onChange={(event) =>
              setConfig({
                ...config,
                runPreLaunch: { ...config.runPreLaunch, command: event.target.value },
              })
            }
          />
          <label className="flex items-center justify-between">
            <span>{t("page.setting.xxmi.builtin.waitForCommand")}</span>
            <Switch
              checked={config.runPreLaunch.wait}
              onCheckedChange={(wait) =>
                setConfig({ ...config, runPreLaunch: { ...config.runPreLaunch, wait } })
              }
            />
          </label>
          <label className="flex items-center justify-between">
            <span>{t("page.setting.xxmi.builtin.customLaunch")}</span>
            <Switch
              checked={config.customLaunch.enabled}
              onCheckedChange={(enabled) =>
                setConfig({ ...config, customLaunch: { ...config.customLaunch, enabled } })
              }
            />
          </label>
          <Input
            value={config.customLaunch.command}
            onChange={(event) =>
              setConfig({
                ...config,
                customLaunch: { ...config.customLaunch, command: event.target.value },
              })
            }
          />
          {config.customLaunch.enabled && (
            <p className="text-muted-foreground">
              {t("page.setting.xxmi.builtin.elevatedCommandWarning")}
            </p>
          )}
          <div className="flex flex-wrap gap-2">
            {["Hook", "Inject", "Bypass"].map((mode) => (
              <Button
                key={mode}
                variant={config.customLaunch.injectMode === mode ? "default" : "outline"}
                onClick={() =>
                  setConfig({
                    ...config,
                    customLaunch: { ...config.customLaunch, injectMode: mode },
                  })
                }
              >
                {mode}
              </Button>
            ))}
          </div>
          <label className="flex items-center justify-between">
            <span>{t("page.setting.xxmi.builtin.postLoad")}</span>
            <Switch
              checked={config.runPostLoad.enabled}
              onCheckedChange={(enabled) =>
                setConfig({ ...config, runPostLoad: { ...config.runPostLoad, enabled } })
              }
            />
          </label>
          <Input
            value={config.runPostLoad.command}
            onChange={(event) =>
              setConfig({
                ...config,
                runPostLoad: { ...config.runPostLoad, command: event.target.value },
              })
            }
          />
          <label className="flex items-center justify-between">
            <span>{t("page.setting.xxmi.builtin.extraLibraries")}</span>
            <Switch
              checked={config.extraLibraries.enabled}
              onCheckedChange={(enabled) =>
                setConfig({ ...config, extraLibraries: { ...config.extraLibraries, enabled } })
              }
            />
          </label>
          <Input
            value={config.extraLibraries.paths?.join(";") ?? ""}
            onChange={(event) =>
              setConfig({
                ...config,
                extraLibraries: {
                  ...config.extraLibraries,
                  paths: event.target.value
                    .split(";")
                    .map((path) => path.trim())
                    .filter(Boolean),
                },
              })
            }
          />
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t("page.setting.xxmi.builtin.iniOptimizer")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <label className="flex items-center justify-between">
            <span>{t("page.setting.xxmi.builtin.optimizeAtLaunch")}</span>
            <Switch
              checked={config.iniOptimizer.enabled}
              onCheckedChange={(enabled) =>
                setConfig({ ...config, iniOptimizer: { ...config.iniOptimizer, enabled } })
              }
            />
          </label>
          <label className="flex items-center justify-between">
            <span>{t("page.setting.xxmi.builtin.resetOptimizerCache")}</span>
            <Switch
              checked={config.iniOptimizer.resetCache}
              onCheckedChange={(resetCache) =>
                setConfig({ ...config, iniOptimizer: { ...config.iniOptimizer, resetCache } })
              }
            />
          </label>
          <div className="flex gap-2">
            <Button
              variant="outline"
              onClickPromise={async () => {
                try {
                  setOptimizationPreview(
                    await XXMI.OptimizeMods({
                      importer,
                      dryRun: true,
                      resetCache: config.iniOptimizer.resetCache,
                    }),
                  );
                } catch (error) {
                  toast.error(toErrorMessage(error));
                }
              }}
            >
              {t("page.setting.xxmi.builtin.previewOptimization")}
            </Button>
            <Button
              disabled={!optimizationPreview?.changes?.length}
              onClickPromise={async () => {
                try {
                  const report = await XXMI.OptimizeMods({
                    importer,
                    dryRun: false,
                    resetCache: config.iniOptimizer.resetCache,
                  });
                  setOptimizationPreview(undefined);
                  toast.success(
                    t("page.setting.xxmi.builtin.optimized", {
                      count: report?.changes?.length ?? 0,
                    }),
                  );
                } catch (error) {
                  toast.error(toErrorMessage(error));
                }
              }}
            >
              {t("page.setting.xxmi.builtin.applyOptimization")}
            </Button>
          </div>
          {optimizationPreview && (
            <div className="max-h-64 space-y-1 overflow-y-auto rounded-md border p-2">
              {optimizationPreview.changes?.length ? (
                optimizationPreview.changes.map((change, index) => (
                  <p key={`${change.path}:${change.line}:${index}`} className="break-all">
                    {change.action}: {change.path}
                    {change.line ? `:${change.line}` : ""} · {change.reason}
                  </p>
                ))
              ) : (
                <p>{t("page.setting.xxmi.builtin.noOptimizationChanges")}</p>
              )}
            </div>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t("page.setting.xxmi.builtin.maintenance")}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClickPromise={async () => {
              try {
                const warnings = await XXMI.RepairRuntime(importer);
                toast.success(t("page.setting.xxmi.builtin.runtimeRepaired"));
                warnings?.forEach((warning) => toast.warning(warning));
              } catch (error) {
                toast.error(toErrorMessage(error));
              }
            }}
          >
            {t("page.setting.xxmi.builtin.repairRuntime")}
          </Button>
          <Button
            variant="outline"
            onClickPromise={async () => {
              try {
                await XXMI.OpenImporterFolder(importer);
              } catch (error) {
                toast.error(toErrorMessage(error));
              }
            }}
          >
            {t("page.setting.xxmi.builtin.openImporterFolder")}
          </Button>
          <Button
            variant="outline"
            onClickPromise={async () => {
              try {
                await XXMI.CreateShortcut(importer);
                refresh();
                toast.success(t("page.setting.xxmi.builtin.shortcutCreated"));
              } catch (error) {
                toast.error(toErrorMessage(error));
              }
            }}
          >
            {t("page.setting.xxmi.builtin.createShortcut")}
          </Button>
        </CardContent>
      </Card>
      {launchGuardDialog}
    </main>
  );
}
