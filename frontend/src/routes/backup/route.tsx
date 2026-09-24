import type { Overview, Status } from "@bindings/backup";
import { backupErrorMessage } from "@renderer/components/backup/errors";
import { backupOverviewKey } from "@renderer/components/backup/queries";
import { cn } from "@renderer/lib/utils";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import {
  CalendarClock,
  ChevronRight,
  DatabaseBackup,
  Folder,
  Gauge,
  History,
  Menu,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/backup")({
  component: RouteComponent,
});

type RunResultEvent = { outcome: string; error?: string };
type RestoreEvent = { outcome: string; destination: string; error?: string };

function RouteComponent() {
  const { t } = useTranslation();
  const location = useLocation();
  const navi = useNavigate();
  const queryClient = useQueryClient();
  const [sidebarOpen, setSidebarOpen] = useState(true);

  useEffect(() => {
    if (location.pathname === "/backup") {
      void navi({ to: "/backup/status", replace: true });
    }
  }, [location.pathname, navi]);

  useEffect(() => {
    const removeStatus = Events.On("backup:status", (event) => {
      const status = event.data as Status;
      queryClient.setQueryData(backupOverviewKey, (previous: Overview | undefined) =>
        previous ? { ...previous, status } : previous,
      );
    });
    const removeResult = Events.On("backup:result", (event) => {
      const result = event.data as RunResultEvent;
      void queryClient.invalidateQueries({ queryKey: ["backup"] });
      if (result.outcome === "completed") toast.success(t("page.backup.toast.completed"));
      if (result.outcome === "unchanged") toast(t("page.backup.toast.unchanged"));
      if (result.outcome === "failed") {
        toast.error(t("page.backup.toast.failed"), {
          description: backupErrorMessage(t, result.error),
        });
      }
    });
    const removeRestore = Events.On("backup:restore", (event) => {
      const result = event.data as RestoreEvent;
      if (result.outcome === "completed") {
        toast.success(t("page.backup.toast.restored"), { description: result.destination });
      }
      if (result.outcome === "failed") {
        toast.error(t("page.backup.toast.restore_failed"), {
          description: backupErrorMessage(t, result.error),
        });
      }
    });
    const removeSetting = Events.On("setting:update", () => {
      void queryClient.invalidateQueries({ queryKey: backupOverviewKey });
    });
    return () => {
      removeStatus();
      removeResult();
      removeRestore();
      removeSetting();
    };
  }, [queryClient, t]);

  const navItems = useMemo(
    () => [
      { icon: Gauge, label: t("page.backup.status.title"), path: "/backup/status" },
      { icon: CalendarClock, label: t("page.backup.schedule.title"), path: "/backup/schedule" },
      { icon: Folder, label: t("page.backup.targets.title"), path: "/backup/targets" },
      { icon: History, label: t("page.backup.snapshots.title"), path: "/backup/snapshots" },
    ],
    [t],
  );

  const activeItem =
    navItems.find((item) => location.pathname === item.path) ??
    navItems.find((item) => location.pathname.startsWith(`${item.path}/`)) ??
    navItems[0];

  return (
    <div className={cn("flex h-full min-h-0 overflow-hidden bg-background text-foreground")}>
      <aside
        className={cn(
          "flex shrink-0 flex-col border-r border-border bg-sidebar",
          sidebarOpen ? "w-60" : "w-0 overflow-hidden border-r-0",
          "md:w-60",
        )}
      >
        <div className="flex items-center gap-2.5 border-b border-sidebar-border px-4 py-4">
          <div className="flex h-7 w-7 items-center justify-center rounded bg-accent/20">
            <DatabaseBackup className="h-3.5 w-3.5 text-accent" />
          </div>
          <span className="text-sm font-semibold tracking-tight text-sidebar-foreground">
            {t("page.backup.title")}
          </span>
        </div>

        <nav className="flex-1 overflow-y-auto px-2 py-3">
          <p className="mb-2 px-2 text-[10px] font-medium tracking-widest text-muted-foreground uppercase">
            {t("page.backup.nav.sections")}
          </p>
          <ul className="space-y-0.5">
            {navItems.map((item) => {
              const isActive = activeItem?.path === item.path;

              return (
                <li key={item.path}>
                  <button
                    type="button"
                    onClick={() => navi({ to: item.path })}
                    className={cn(
                      "flex w-full items-center justify-between gap-2 rounded-md px-2 py-2 text-sm transition-colors",
                      isActive
                        ? "bg-sidebar-accent text-sidebar-accent-foreground"
                        : "text-sidebar-foreground hover:bg-sidebar-accent",
                    )}
                  >
                    <div className="flex min-w-0 items-center gap-2.5">
                      <span
                        className={cn(
                          "flex h-5 w-5 shrink-0 items-center justify-center rounded transition-colors",
                          isActive
                            ? "bg-accent/20 text-accent"
                            : "bg-secondary text-muted-foreground",
                        )}
                      >
                        <item.icon className="h-3.5 w-3.5" />
                      </span>
                      <span className="truncate text-xs">{item.label}</span>
                    </div>
                    {isActive && <ChevronRight className="h-3 w-3 shrink-0 text-accent" />}
                  </button>
                </li>
              );
            })}
          </ul>
        </nav>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-10 shrink-0 items-center gap-3 border-b border-border px-4">
          <button
            type="button"
            onClick={() => setSidebarOpen((open) => !open)}
            className="rounded p-1.5 transition-colors hover:bg-secondary md:hidden"
            aria-label={t("page.backup.nav.toggle")}
          >
            {sidebarOpen ? (
              <X className="h-4 w-4 text-muted-foreground" />
            ) : (
              <Menu className="h-4 w-4 text-muted-foreground" />
            )}
          </button>

          <div className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
            <span className="font-medium text-foreground">{t("page.backup.title")}</span>
            {activeItem && (
              <>
                <span>/</span>
                <span className="text-foreground">{activeItem.label}</span>
              </>
            )}
          </div>
        </header>

        <div className="min-h-0 flex-1 scrollbar-gutter-stable overflow-y-auto">
          <div className="mx-auto min-h-full w-full max-w-2xl">
            <Outlet />
          </div>
        </div>
      </div>
    </div>
  );
}
