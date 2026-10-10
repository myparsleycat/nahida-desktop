import { Updater } from "@bindings/infra";
import { DEFAULT_BG } from "@renderer/const";
import { cn } from "@renderer/lib/utils";
import { globalStore, useGlobalStore } from "@renderer/store/global";
import { toErrorMessage } from "@shared/utils";
import { createFileRoute, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import {
  ArrowUpDown,
  BotIcon,
  ChevronRight,
  GamepadIcon,
  HardDrive,
  Menu,
  Network,
  ServerCrash,
  Settings,
  User,
  Wrench,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const location = useLocation();
  const navi = useNavigate();
  const appStatus = useGlobalStore((state) => state.appStatus);
  const updaterChecking = useGlobalStore((state) => state.updaterChecking);
  const [sidebarOpen, setSidebarOpen] = useState(true);

  useEffect(() => {
    if (location.pathname === "/setting") {
      void navi({ to: "/setting/gen", replace: true });
    }
  }, [location.pathname, navi]);

  const navItems = useMemo(
    () => [
      { icon: Settings, label: t("page.setting.tabs.general"), path: "/setting/gen" },
      { icon: GamepadIcon, label: t("page.setting.tabs.mod"), path: "/setting/mod" },
      { icon: HardDrive, label: t("page.setting.tabs.drive"), path: "/setting/drive" },
      { icon: User, label: t("page.setting.tabs.account"), path: "/setting/acc" },
      { icon: ArrowUpDown, label: t("page.setting.tabs.transfer"), path: "/setting/transfer" },
      { icon: Network, label: t("page.setting.tabs.network"), path: "/setting/network" },
      { icon: Wrench, label: t("page.setting.tabs.tools"), path: "/setting/tools" },
      { icon: BotIcon, label: t("page.agent.title"), path: "/setting/agent" },
      { icon: ServerCrash, label: t("page.setting.tabs.advanced"), path: "/setting/adv" },
    ],
    [t],
  );

  const activeItem =
    navItems.find((item) => location.pathname === item.path) ??
    navItems.find((item) => location.pathname.startsWith(`${item.path}/`)) ??
    navItems[0];

  return (
    <div className="flex h-full min-h-0 overflow-hidden text-foreground">
      <aside
        className={cn(
          "glass-pane flex shrink-0 flex-col border-r border-border",
          sidebarOpen ? "w-64" : "w-0 overflow-hidden border-r-0",
          "md:w-64",
        )}
      >
        <div className="flex items-center gap-2.5 border-b border-sidebar-border px-4 py-4">
          <div className="flex h-7 w-7 items-center justify-center rounded bg-accent/20">
            <Settings className="h-3.5 w-3.5 text-accent" />
          </div>
          <span className="text-sm font-semibold tracking-tight text-sidebar-foreground">
            {t("page.setting.title")}
          </span>
        </div>

        <nav className="flex-1 overflow-y-auto px-2 py-3">
          <p className="mb-2 px-2 text-[10px] font-medium tracking-widest text-muted-foreground uppercase">
            Sections
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

        <div className="border-t border-sidebar-border px-4 py-3 text-[11px] text-muted-foreground">
          v{appStatus?.version}
          {" · "}
          <button
            type="button"
            disabled={updaterChecking}
            className="cursor-pointer underline underline-offset-2 hover:text-foreground disabled:cursor-default disabled:opacity-60"
            onClick={() => {
              toast.promise(checkForUpdates(), {
                loading: t("updater.status.checking"),
                success: (status) =>
                  status.updateDownloaded
                    ? t("updater.status.downloaded")
                    : status.updateAvailable
                      ? t("updater.toast.available.title")
                      : t("updater.toast.notAvailable.title"),
                error: (error: unknown) =>
                  t("updater.status.failed", { message: toErrorMessage(error) }),
              });
            }}
          >
            {t("updater.actions.check")}
          </button>
        </div>
      </aside>

      <div className={cn("flex min-w-0 flex-1 flex-col", DEFAULT_BG)}>
        <header className="flex h-10 shrink-0 items-center gap-3 border-b border-border px-4">
          <button
            type="button"
            onClick={() => setSidebarOpen((open) => !open)}
            className="rounded p-1.5 transition-colors hover:bg-secondary md:hidden"
            aria-label="Toggle setting navigation"
          >
            {sidebarOpen ? (
              <X className="h-4 w-4 text-muted-foreground" />
            ) : (
              <Menu className="h-4 w-4 text-muted-foreground" />
            )}
          </button>

          <div className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
            <span className="font-medium text-foreground">{t("page.setting.title")}</span>
            {activeItem && (
              <>
                <span>/</span>
                <span className="text-foreground">{activeItem.label}</span>
              </>
            )}
          </div>
        </header>

        <div className="min-h-0 flex-1 scrollbar-gutter-stable overflow-y-auto">
          <div className="mx-auto min-h-full w-full max-w-xl">
            <Outlet />
          </div>
        </div>
      </div>
    </div>
  );
}

// In auto mode CheckForUpdates also awaits the download, so the check counts as
// finished once the backend starts downloading after its checking phase.
// A settled checking flag alone can still describe a failed candidate refresh.
async function checkForUpdates() {
  const check = Updater.CheckForUpdates(true);
  let stopWaiting = () => {};
  const releaseFound = new Promise<void>((resolve) => {
    let sawChecking = false;
    stopWaiting = globalStore.subscribe((state) => {
      sawChecking ||= state.updaterChecking;
      if (
        sawChecking &&
        !state.updaterChecking &&
        state.updateAvailable &&
        state.updaterDownloading
      ) {
        resolve();
      }
    });
  });

  try {
    await Promise.race([check, releaseFound]);
  } finally {
    stopWaiting();
    // A download failure after this point is reported by the updater status.
    check.catch(() => {});
  }
  return Updater.GetStatus();
}
