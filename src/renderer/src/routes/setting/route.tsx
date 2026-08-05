import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { cn } from "@renderer/lib/utils";
import { useGlobalStore } from "@renderer/store/global";
import { createFileRoute, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import {
  ArrowUpDown,
  ChevronRight,
  GamepadIcon,
  HardDrive,
  Menu,
  PackageIcon,
  ServerCrash,
  Settings,
  User,
  Wrench,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const { Titlebar, screenHeight } = useTitlebar();
  const location = useLocation();
  const navi = useNavigate();
  const appStatus = useGlobalStore((state) => state.appStatus);
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
      { icon: Wrench, label: t("page.setting.tabs.tools"), path: "/setting/tools" },
      { icon: PackageIcon, label: "XXMI", path: "/setting/xxmi" },
      { icon: HardDrive, label: t("page.setting.tabs.drive"), path: "/setting/drive" },
      { icon: User, label: t("page.setting.tabs.account"), path: "/setting/acc" },
      { icon: ArrowUpDown, label: t("page.setting.tabs.transfer"), path: "/setting/transfer" },
      { icon: ServerCrash, label: t("page.setting.tabs.advanced"), path: "/setting/adv" },
    ],
    [t],
  );

  const activeItem =
    navItems.find((item) => location.pathname === item.path) ??
    navItems.find((item) => location.pathname.startsWith(`${item.path}/`)) ??
    navItems[0];

  return (
    <>
      <Titlebar />

      <div className={cn("flex h-full min-h-0 overflow-hidden bg-background text-foreground")}>
        <aside
          className={cn(
            "flex shrink-0 flex-col border-r border-border bg-sidebar",
            sidebarOpen ? "w-60" : "w-0 overflow-hidden border-r-0",
            "md:w-60",
            screenHeight,
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
          </div>
        </aside>

        <div className="flex min-w-0 flex-1 flex-col">
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

          <ScrollArea className="min-h-0 flex-1">
            <div className="mx-auto min-h-full w-full max-w-xl">
              <Outlet />
            </div>
          </ScrollArea>
        </div>
      </div>
    </>
  );
}
