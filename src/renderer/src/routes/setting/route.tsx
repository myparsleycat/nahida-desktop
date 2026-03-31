import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { cn } from "@renderer/lib/utils";
import { useGlobalStore } from "@renderer/store/global";
import { supportsWindowsDesktopFeatures } from "@shared/platform";
import { createFileRoute, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import {
  ArrowUpDown,
  ChevronRight,
  GamepadIcon,
  Menu,
  PackageIcon,
  ServerCrash,
  Settings,
  User,
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
  const hasWindowsDesktopFeatures = supportsWindowsDesktopFeatures(appStatus?.platform);
  const [sidebarOpen, setSidebarOpen] = useState(true);

  useEffect(() => {
    if (location.pathname === "/setting") {
      navi({ to: "/setting/gen", replace: true });
      return;
    }

    if (
      appStatus &&
      !hasWindowsDesktopFeatures &&
      ["/setting/mod", "/setting/xxmi"].includes(location.pathname)
    ) {
      navi({ to: "/setting/gen", replace: true });
    }
  }, [appStatus, hasWindowsDesktopFeatures, location.pathname, navi]);

  const navItems = useMemo(
    () =>
      [
        { icon: Settings, label: t("page.setting.tabs.general"), path: "/setting/gen" },
        { icon: GamepadIcon, label: t("page.setting.tabs.mod"), path: "/setting/mod" },
        { icon: PackageIcon, label: "XXMI", path: "/setting/xxmi" },
        { icon: User, label: t("page.setting.tabs.account"), path: "/setting/acc" },
        { icon: ArrowUpDown, label: t("page.setting.tabs.transfer"), path: "/setting/transfer" },
        { icon: ServerCrash, label: t("page.setting.tabs.advanced"), path: "/setting/adv" },
      ].filter(
        (item) =>
          hasWindowsDesktopFeatures || !["/setting/mod", "/setting/xxmi"].includes(item.path),
      ),
    [hasWindowsDesktopFeatures, t],
  );

  const activeItem =
    navItems.find((item) => location.pathname === item.path) ??
    navItems.find((item) => location.pathname.startsWith(`${item.path}/`)) ??
    navItems[0];

  return (
    <>
      <Titlebar />

      <div className={cn("flex h-full min-h-0 bg-background text-foreground overflow-hidden")}>
        <aside
          className={cn(
            "flex flex-col border-r border-border bg-sidebar shrink-0",
            sidebarOpen ? "w-60" : "w-0 overflow-hidden border-r-0",
            "md:w-60",
            screenHeight,
          )}
        >
          <div className="flex items-center gap-2.5 px-4 py-4 border-b border-sidebar-border">
            <div className="flex h-7 w-7 items-center justify-center rounded bg-accent/20">
              <Settings className="h-3.5 w-3.5 text-accent" />
            </div>
            <span className="text-sm font-semibold text-sidebar-foreground tracking-tight">
              {t("page.setting.title")}
            </span>
          </div>

          <nav className="flex-1 overflow-y-auto py-3 px-2">
            <p className="px-2 mb-2 text-[10px] font-medium uppercase tracking-widest text-muted-foreground">
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
                        "w-full flex items-center justify-between gap-2 rounded-md px-2 py-2 text-sm transition-colors",
                        isActive
                          ? "bg-sidebar-accent text-sidebar-accent-foreground"
                          : "text-sidebar-foreground hover:bg-sidebar-accent",
                      )}
                    >
                      <div className="flex items-center gap-2.5 min-w-0">
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
                      {isActive && <ChevronRight className="h-3 w-3 text-accent shrink-0" />}
                    </button>
                  </li>
                );
              })}
            </ul>
          </nav>

          <div className="px-4 py-3 border-t border-sidebar-border text-[11px] text-muted-foreground">
            v{appStatus?.version}
          </div>
        </aside>

        <div className="flex flex-1 flex-col min-w-0">
          <header className="flex items-center gap-3 px-4 h-10 border-b border-border shrink-0">
            <button
              type="button"
              onClick={() => setSidebarOpen((open) => !open)}
              className="md:hidden p-1.5 rounded hover:bg-secondary transition-colors"
              aria-label="Toggle setting navigation"
            >
              {sidebarOpen ? (
                <X className="h-4 w-4 text-muted-foreground" />
              ) : (
                <Menu className="h-4 w-4 text-muted-foreground" />
              )}
            </button>

            <div className="flex items-center gap-1.5 text-xs text-muted-foreground font-mono">
              <span className="text-foreground font-medium">{t("page.setting.title")}</span>
              {activeItem && (
                <>
                  <span>/</span>
                  <span className="text-foreground">{activeItem.label}</span>
                </>
              )}
            </div>
          </header>

          <ScrollArea className="flex-1 min-h-0">
            <div className="min-h-full w-full max-w-xl mx-auto">
              <Outlet />
            </div>
          </ScrollArea>
        </div>
      </div>
    </>
  );
}
