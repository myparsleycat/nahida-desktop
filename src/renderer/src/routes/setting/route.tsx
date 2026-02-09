import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute, Link, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import {
  Bell,
  Database,
  Folder,
  Globe,
  RefreshCw,
  Settings,
  User,
  GamepadIcon,
} from "lucide-react";
import { useEffect } from "react";
import { useGlobalStore } from "@renderer/store/global";
import { useTranslation } from "react-i18next";
import { ScrollArea } from "@renderer/components/ui/scroll-area";

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
  }, [location.pathname]);

  const navItems = [
    { icon: Settings, label: t("page.setting.tabs.general"), path: "/setting/gen" },
    { icon: GamepadIcon, label: t("page.setting.tabs.mod"), path: "/setting/mod" },
    { icon: User, label: t("page.setting.tabs.account"), path: "/setting/acc" },
    // { icon: RefreshCw, label: "동기화", path: "/setting/sync" },
    // { icon: Database, label: "백업", path: "/setting/bak" },
    // { icon: Folder, label: "공간", path: "/setting/space" },
    { icon: Globe, label: t("page.setting.tabs.network"), path: "/setting/net" },
    // { icon: Bell, label: "알림", path: "/setting/noti" },
  ];

  return (
    <div className="flex flex-col h-full relative">
      <Titlebar title={{ text: t("page.setting.title"), position: "center" }} />

      <nav className="border-b">
        <div className="flex items-center justify-center gap-1 p-3">
          {navItems.map((item, index) => (
            <button
              key={index}
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
