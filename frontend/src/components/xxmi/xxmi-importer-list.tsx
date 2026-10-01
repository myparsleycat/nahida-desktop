import { Collapsible } from "@base-ui/react/collapsible";
import { XXMI } from "@bindings/xxmi";
import { GameIcon } from "@renderer/components/game-icon";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { useLaunchGuard } from "@renderer/hooks/use-launch-guard";
import { cn } from "@renderer/lib/utils";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { partition, uniqBy } from "es-toolkit";
import { ChevronRightIcon, LayoutDashboardIcon, PlayIcon, RefreshCwIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

type UpdateStatus = NonNullable<Awaited<ReturnType<typeof XXMI.CheckUpdates>>>[number];

const importerKeys = ["GIMI", "SRMI", "HIMI", "ZZMI", "WWMI", "EFMI"] as const;

export function XXMIImporterList() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const pathname = useLocation().pathname.replace(/\/$/, "");
  const queryClient = useQueryClient();
  const { startImporter, launchGuardDialog } = useLaunchGuard();
  const { data: overview } = useQuery({ queryKey: ["xxmi:overview"], queryFn: XXMI.GetOverview });
  const updates = useXXMIUpdates(overview?.configured ?? false);
  const pendingUpdates = installableUpdates(updates);
  const [showUninstalled, setShowUninstalled] = useState(false);
  const [installedKeys, uninstalledKeys] = partition(importerKeys, (key) =>
    Boolean(overview?.importers?.some((entry) => entry.key === key)),
  );
  // Keep the section open while its importer page is shown so the active row stays visible.
  const uninstalledActive =
    overview !== undefined && uninstalledKeys.some((key) => pathname === `/xxmi/${key}`);

  const renderImporter = (key: (typeof importerKeys)[number]) => {
    const importer = overview?.importers?.find((entry) => entry.key === key);
    const available =
      updates?.some((entry) => entry.importer === key && entry.available) ||
      importer?.updateAvailable;
    const active = pathname === `/xxmi/${key}`;
    return (
      <li
        key={key}
        className={cn(
          "flex items-center gap-1 rounded-md pr-1.5 transition-colors",
          active
            ? "bg-sidebar-accent text-sidebar-accent-foreground"
            : "text-sidebar-foreground hover:bg-sidebar-accent",
        )}
      >
        <button
          type="button"
          aria-current={active ? "page" : undefined}
          onClick={() => void navigate({ to: "/xxmi/$importer", params: { importer: key } })}
          className="flex min-w-0 flex-1 items-center gap-2.5 p-2 text-left"
        >
          <GameIcon gameName={key} className="size-8 shrink-0 rounded-md" />
          <div className="min-w-0 flex-1 space-y-0.5">
            <div className="flex flex-wrap items-center gap-1">
              <span className="text-sm font-medium">{key}</span>
              {importer && (
                <Badge variant="secondary">
                  {importer.mode === "legacy" ? "3DMigoto" : "XXMI"}
                </Badge>
              )}
              {available && <Badge>{t("page.setting.xxmi.builtin.updateAvailable")}</Badge>}
              {importer?.running && (
                <Badge variant="outline">{t("page.setting.xxmi.builtin.running")}</Badge>
              )}
            </div>
            <p className="truncate text-xs text-muted-foreground">
              {importer?.packageInfo.deployed_version ||
                t("page.setting.xxmi.builtin.notInstalled")}
              {importer?.customDll && (
                <>
                  {" · "}
                  <span title={t("page.setting.xxmi.builtin.customDllDescription")}>
                    {t("page.setting.xxmi.builtin.customDll")}
                  </span>
                </>
              )}
            </p>
          </div>
        </button>
        {importer && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("page.setting.xxmi.builtin.launch")}
            title={t("page.setting.xxmi.builtin.launch")}
            disabled={importer.running}
            onClickPromise={async () => {
              try {
                await startImporter(key);
              } catch (error) {
                toast.error(toErrorMessage(error));
              }
            }}
          >
            <PlayIcon />
          </Button>
        )}
      </li>
    );
  };

  return (
    <aside className="flex w-64 shrink-0 flex-col border-r border-border bg-sidebar">
      <nav className="flex-1 space-y-3 overflow-y-auto px-2 py-3">
        <button
          type="button"
          aria-current={pathname === "/xxmi" ? "page" : undefined}
          onClick={() => void navigate({ to: "/xxmi" })}
          className={cn(
            "flex w-full items-center gap-2.5 rounded-md p-2 text-left text-sm transition-colors",
            pathname === "/xxmi"
              ? "bg-sidebar-accent text-sidebar-accent-foreground"
              : "text-sidebar-foreground hover:bg-sidebar-accent",
          )}
        >
          <LayoutDashboardIcon className="size-4 shrink-0 text-muted-foreground" />
          <span className="flex-1 truncate">{t("page.setting.xxmi.builtin.manage")}</span>
          {pendingUpdates.length > 0 && <Badge>{pendingUpdates.length}</Badge>}
        </button>

        <div>
          <div className="mb-1 flex items-center justify-between px-2">
            <span className="text-[10px] font-medium tracking-widest text-muted-foreground uppercase">
              {t("page.setting.xxmi.builtin.importers")}
            </span>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={t("page.setting.xxmi.builtin.checkUpdates")}
              title={t("page.setting.xxmi.builtin.checkUpdates")}
              onClickPromise={async () => {
                try {
                  const result = await XXMI.CheckUpdates(true);
                  void queryClient.invalidateQueries({
                    predicate: (query) =>
                      typeof query.queryKey[0] === "string" &&
                      query.queryKey[0].startsWith("xxmi:"),
                  });
                  if (installableUpdates(result).length === 0) {
                    toast.success(t("page.setting.xxmi.builtin.noUpdates"));
                  }
                } catch (error) {
                  toast.error(toErrorMessage(error));
                }
              }}
            >
              <RefreshCwIcon />
            </Button>
          </div>
          <ul className="space-y-0.5">{installedKeys.map(renderImporter)}</ul>
          {uninstalledKeys.length > 0 && (
            <Collapsible.Root
              open={showUninstalled || uninstalledActive}
              onOpenChange={setShowUninstalled}
              className="mt-2"
            >
              <Collapsible.Trigger className="group flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-sidebar-accent">
                <ChevronRightIcon className="size-3.5 shrink-0 transition-transform group-data-panel-open:rotate-90" />
                <span className="flex-1 text-left">
                  {t("page.setting.xxmi.builtin.notInstalled")}
                </span>
                <span className="tabular-nums">{uninstalledKeys.length}</span>
              </Collapsible.Trigger>
              <Collapsible.Panel>
                <ul className="mt-0.5 space-y-0.5">{uninstalledKeys.map(renderImporter)}</ul>
              </Collapsible.Panel>
            </Collapsible.Root>
          )}
        </div>
      </nav>
      {launchGuardDialog}
    </aside>
  );
}

export function useXXMIUpdates(enabled: boolean) {
  return useQuery({
    queryKey: ["xxmi:updates"],
    queryFn: () => XXMI.CheckUpdates(false),
    enabled,
    staleTime: 60 * 60 * 1000,
  }).data;
}

// A shared package appears once per importer; only unpinned ones can be installed in bulk.
export function installableUpdates(updates: UpdateStatus[] | null | undefined) {
  return uniqBy(
    updates?.filter((entry) => entry.available && !entry.pinned) ?? [],
    (entry) => entry.package,
  );
}
