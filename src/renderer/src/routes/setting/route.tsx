import { Titlebar } from "@renderer/components/titlebar";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useGlobalStore } from "@renderer/store/global";
import { createFileRoute, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { GamepadIcon, Globe, PackageIcon, ServerCrash, Settings, User } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const location = useLocation();
  const navi = useNavigate();
  const appStatus = useGlobalStore((state) => state.appStatus);

  useEffect(() => {
    if (location.pathname === "/setting") {
      navi({ to: "/setting/gen" });
    }
  }, [navi, location.pathname]);

  const navItems = [
    { icon: Settings, label: t("page.setting.tabs.general"), path: "/setting/gen" },
    { icon: GamepadIcon, label: t("page.setting.tabs.mod"), path: "/setting/mod" },
    { icon: PackageIcon, label: "XXMI", path: "/setting/xxmi" },
    { icon: User, label: t("page.setting.tabs.account"), path: "/setting/acc" },
    { icon: Globe, label: t("page.setting.tabs.network"), path: "/setting/net" },
    { icon: ServerCrash, label: t("page.setting.tabs.advanced"), path: "/setting/adv" },
  ];

  return (
    <div className="flex flex-col h-full relative">
      <Titlebar title={{ text: t("page.setting.title"), position: "center" }} />

      <nav className="border-b">
        <div className="flex items-center justify-center gap-4 p-3">
          {navItems.map((item) => (
            <button
              key={item.path}
              type="button"
              onClick={() => navi({ to: item.path })}
              className={`flex flex-col items-center gap-2 transition-colors size-14 ${
                location.pathname === item.path
                  ? "text-foreground"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              <item.icon className="h-6 w-6" />
              <span className="text-sm">{item.label}</span>
            </button>
          ))}
        </div>
      </nav>

      <ScrollArea className="overflow-y-auto">
        <div className="flex-1 flex flex-col p-2 overflow-hidden">
          <Outlet />
        </div>
      </ScrollArea>

      <div className="absolute top-0 right-2 pointer-events-none">
        <span className="text-xs text-muted-foreground">v{appStatus?.version}</span>
      </div>
    </div>
  );
}
