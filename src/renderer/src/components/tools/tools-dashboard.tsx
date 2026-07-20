import FourThousandOneFixer from "@renderer/components/tools/4001-fixer";
import BodyShapeTool from "@renderer/components/tools/body-shape/body-shape";
import ModBisect from "@renderer/components/tools/mod-bisect";
import StaticGlbConverter from "@renderer/components/tools/static-glb-converter";
import TextureResizer from "@renderer/components/tools/texture-resizer";
import TogglePersistence from "@renderer/components/tools/toggle-persistence";
import ToggleViewerGenerator from "@renderer/components/tools/toggle-viewer-generator";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { cn } from "@renderer/lib/utils";
import { Link } from "@tanstack/react-router";
import { ChevronRight, ExternalLink, Menu, Wrench, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import FixToolManager from "./fix-tool-manger";

type ToolPage =
  | {
      nameKey: string;
      initials: string;
      component: () => React.ReactNode;
      path?: never;
    }
  | {
      nameKey: string;
      initials: string;
      path: string;
      component?: never;
    };

const toolPages: ToolPage[] = [
  {
    nameKey: "page.tools.4001_fixer.title",
    initials: "41",
    component: () => <FourThousandOneFixer />,
  },
  {
    nameKey: "page.setting.xxmi.persistToggles",
    initials: "TP",
    component: () => <TogglePersistence />,
  },
  {
    nameKey: "page.tools.toggle_viewer_generator.title",
    initials: "TV",
    component: () => <ToggleViewerGenerator />,
  },
  {
    nameKey: "page.tools.static_glb_converter.title",
    initials: "SG",
    component: () => <StaticGlbConverter />,
  },
  {
    nameKey: "page.tools.texture_resizer.title",
    initials: "TR",
    component: () => <TextureResizer />,
  },
  {
    nameKey: "page.tools.fix-tool-manager.title",
    initials: "FT",
    component: () => <FixToolManager />,
  },
  {
    nameKey: "page.tools.mod_bisect.title",
    initials: "MB",
    component: () => <ModBisect />,
  },
  {
    nameKey: "page.tools.body_shape.title",
    initials: "BS",
    component: () => <BodyShapeTool />,
  },
];

export default function ToolsPage() {
  const { screenHeight } = useTitlebar();
  const { t } = useTranslation();
  const [activeIndex, setActiveIndex] = useState<number | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(true);

  const activeTool = activeIndex !== null ? toolPages[activeIndex] : null;

  return (
    <div
      className={cn("flex h-full min-h-0 overflow-hidden bg-background font-sans text-foreground")}
    >
      <aside
        className={cn(
          "flex shrink-0 flex-col border-r border-border bg-sidebar md:w-56",
          sidebarOpen ? "w-56" : "w-0 overflow-hidden",
          screenHeight,
        )}
      >
        <div className="flex items-center gap-2.5 border-b border-sidebar-border px-4 py-4">
          <div className="flex h-7 w-7 items-center justify-center rounded bg-accent/20">
            <Wrench className="h-3.5 w-3.5 text-accent" />
          </div>
          <span className="text-sm font-semibold tracking-tight text-sidebar-foreground">
            Tools
          </span>
        </div>

        <nav className="flex-1 overflow-y-auto px-2 py-3">
          <p className="mb-2 px-2 text-[10px] font-medium tracking-widest text-muted-foreground uppercase">
            {t("page.tools.dashboard.tools_label")}
          </p>
          <ul className="space-y-0.5">
            {toolPages.map((tool, index) => {
              const isActive = activeIndex === index;
              const isExternal = !!tool.path;
              const toolName = t(tool.nameKey);

              if (isExternal) {
                return (
                  <li key={tool.nameKey}>
                    <Link
                      to={tool.path}
                      className="group flex items-center justify-between gap-2 rounded-md px-2 py-2 text-sm text-sidebar-foreground transition-colors hover:bg-sidebar-accent"
                    >
                      <div className="flex items-center gap-2.5">
                        <span className="flex h-5 w-5 items-center justify-center rounded bg-secondary font-mono text-[9px] font-bold text-muted-foreground">
                          {tool.initials}
                        </span>
                        <span className="text-xs">{toolName}</span>
                      </div>
                      <ExternalLink className="h-3 w-3 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                    </Link>
                  </li>
                );
              }

              return (
                <li key={tool.nameKey}>
                  <button
                    onClick={() => setActiveIndex(index)}
                    className={`flex w-full items-center justify-between gap-2 rounded-md px-2 py-2 text-sm transition-colors ${
                      isActive
                        ? "bg-sidebar-accent text-sidebar-accent-foreground"
                        : "text-sidebar-foreground hover:bg-sidebar-accent"
                    }`}
                  >
                    <div className="flex items-center gap-2.5">
                      <span
                        className={`flex h-5 w-5 items-center justify-center rounded font-mono text-[9px] font-bold transition-colors ${
                          isActive
                            ? "bg-accent text-accent-foreground"
                            : "bg-secondary text-muted-foreground"
                        }`}
                      >
                        {tool.initials}
                      </span>
                      <span className="text-xs">{toolName}</span>
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
            onClick={() => setSidebarOpen((v) => !v)}
            className="rounded p-1.5 transition-colors hover:bg-secondary md:hidden"
            aria-label={t("page.tools.dashboard.toggle_sidebar")}
          >
            {sidebarOpen ? (
              <X className="h-4 w-4 text-muted-foreground" />
            ) : (
              <Menu className="h-4 w-4 text-muted-foreground" />
            )}
          </button>

          <div className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
            <span className="font-medium text-foreground">
              {t("page.tools.dashboard.tools_label")}
            </span>
            {activeTool && (
              <>
                <span>/</span>
                <span className="text-foreground">{t(activeTool.nameKey)}</span>
              </>
            )}
          </div>
        </header>

        <main className="min-h-0 flex-1 overflow-hidden">
          {activeTool && activeTool.component ? (
            <div className="h-full min-h-0">{activeTool.component()}</div>
          ) : (
            <div className="mx-auto h-full max-w-3xl space-y-6 overflow-y-auto">
              <div className="grid grid-cols-1 gap-3 p-4 sm:grid-cols-2">
                {toolPages.map((tool, index) => {
                  const isExternal = !!tool.path;
                  const toolName = t(tool.nameKey);

                  if (isExternal) {
                    return (
                      <Link key={tool.nameKey} to={tool.path}>
                        <div className="group flex cursor-pointer items-center justify-between rounded-lg border border-border bg-card p-4 transition-all hover:border-accent/40 hover:bg-card/80">
                          <div className="flex items-center gap-3">
                            <div className="flex h-9 w-9 items-center justify-center rounded-md bg-secondary font-mono text-sm font-bold text-muted-foreground">
                              {tool.initials}
                            </div>
                            <div>
                              <p className="text-sm font-medium text-foreground">{toolName}</p>
                              <p className="mt-0.5 text-xs text-muted-foreground">
                                {t("page.tools.dashboard.external_page")}
                              </p>
                            </div>
                          </div>
                          <ExternalLink className="h-4 w-4 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                        </div>
                      </Link>
                    );
                  }

                  return (
                    <button
                      key={tool.nameKey}
                      onClick={() => setActiveIndex(index)}
                      className="group flex items-center justify-between rounded-lg border border-border bg-card p-4 text-left transition-all hover:border-accent/40 hover:bg-card/80"
                    >
                      <div className="flex items-center gap-3">
                        <div className="flex h-9 w-9 items-center justify-center rounded-md bg-secondary font-mono text-sm font-bold text-muted-foreground transition-colors group-hover:bg-accent/20 group-hover:text-accent">
                          {tool.initials}
                        </div>
                        <div>
                          <p className="text-sm font-medium text-foreground">{toolName}</p>
                          <p className="mt-0.5 text-xs text-muted-foreground">
                            {t("page.tools.dashboard.inline_tool")}
                          </p>
                        </div>
                      </div>
                      <ChevronRight className="h-4 w-4 text-muted-foreground opacity-0 transition-all group-hover:text-accent group-hover:opacity-100" />
                    </button>
                  );
                })}
              </div>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
