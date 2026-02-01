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
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { Button } from "@renderer/components/ui/button";

export const Route = createFileRoute("/setting")({
  component: RouteComponent,
});

function RouteComponent() {
  const location = useLocation();
  const navi = useNavigate();
  const appStatus = useGlobalStore((state) => state.appStatus);

  const [anim1] = useAutoAnimate({ duration: 100 });

  useEffect(() => {
    if (location.pathname === "/setting") {
      navi({ to: "/setting/gen" });
    }
  }, [location.pathname]);

  const navItems = [
    { icon: Settings, label: "일반", path: "/setting/gen" },
    { icon: GamepadIcon, label: "모드", path: "/setting/mod" },
    { icon: User, label: "계정", path: "/setting/acc" },
    // { icon: RefreshCw, label: "동기화", path: "/setting/sync" },
    // { icon: Database, label: "백업", path: "/setting/bak" },
    // { icon: Folder, label: "공간", path: "/setting/space" },
    { icon: Globe, label: "네트워크", path: "/setting/net" },
    // { icon: Bell, label: "알림", path: "/setting/noti" },
  ];

  return (
    <div className="flex flex-col h-full relative">
      <Titlebar title={{ text: "설정", position: "center" }} />

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

      <div className="flex-1 flex flex-col p-2" ref={anim1}>
        <Outlet />
      </div>

      <div className="absolute bottom-4 right-4 pointer-events-none">
        <span className="text-xs text-muted-foreground">v{appStatus?.version}</span>
      </div>
    </div>
  );
}
