import D3D11Builder from "@renderer/components/tools/d3d11-builder";
import StaticGlbConverter from "@renderer/components/tools/static-glb-converter";
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
    nameKey: "page.tools.d3d11_builder.title",
    initials: "D3",
    component: () => <D3D11Builder />,
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
    nameKey: "page.tools.fix-tool-manager.title",
    initials: "FT",
    component: () => <FixToolManager />,
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
      className={cn("flex h-full min-h-0 bg-background text-foreground font-sans overflow-hidden")}
    >
      <aside
        className={cn(
          "flex flex-col border-r border-border bg-sidebar md:w-56 shrink-0",
          sidebarOpen ? "w-56" : "w-0 overflow-hidden",
          screenHeight,
        )}
      >
        <div className="flex items-center gap-2.5 px-4 py-4 border-b border-sidebar-border">
          <div className="flex h-7 w-7 items-center justify-center rounded bg-accent/20">
            <Wrench className="h-3.5 w-3.5 text-accent" />
          </div>
          <span className="text-sm font-semibold text-sidebar-foreground tracking-tight">
            {t("page.tools.dashboard.sidebar_title")}
          </span>
        </div>

        <nav className="flex-1 overflow-y-auto py-3 px-2">
          <p className="px-2 mb-2 text-[10px] font-medium uppercase tracking-widest text-muted-foreground">
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
                      className="flex items-center justify-between gap-2 rounded-md px-2 py-2 text-sm text-sidebar-foreground hover:bg-sidebar-accent transition-colors group"
                    >
                      <div className="flex items-center gap-2.5">
                        <span className="flex h-5 w-5 items-center justify-center rounded bg-secondary text-[9px] font-bold text-muted-foreground font-mono">
                          {tool.initials}
                        </span>
                        <span className="text-xs">{toolName}</span>
                      </div>
                      <ExternalLink className="h-3 w-3 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                    </Link>
                  </li>
                );
              }

              return (
                <li key={tool.nameKey}>
                  <button
                    onClick={() => setActiveIndex(index)}
                    className={`w-full flex items-center justify-between gap-2 rounded-md px-2 py-2 text-sm transition-colors ${
                      isActive
                        ? "bg-sidebar-accent text-sidebar-accent-foreground"
                        : "text-sidebar-foreground hover:bg-sidebar-accent"
                    }`}
                  >
                    <div className="flex items-center gap-2.5">
                      <span
                        className={`flex h-5 w-5 items-center justify-center rounded text-[9px] font-bold font-mono transition-colors ${
                          isActive
                            ? "bg-accent text-accent-foreground"
                            : "bg-secondary text-muted-foreground"
                        }`}
                      >
                        {tool.initials}
                      </span>
                      <span className="text-xs">{toolName}</span>
                    </div>
                    {isActive && <ChevronRight className="h-3 w-3 text-accent shrink-0" />}
                  </button>
                </li>
              );
            })}
          </ul>
        </nav>
      </aside>

      <div className="flex flex-1 flex-col min-w-0">
        <header className="flex items-center gap-3 px-4 h-10 border-b border-border shrink-0">
          <button
            onClick={() => setSidebarOpen((v) => !v)}
            className="md:hidden p-1.5 rounded hover:bg-secondary transition-colors"
            aria-label={t("page.tools.dashboard.toggle_sidebar")}
          >
            {sidebarOpen ? (
              <X className="h-4 w-4 text-muted-foreground" />
            ) : (
              <Menu className="h-4 w-4 text-muted-foreground" />
            )}
          </button>

          <div className="flex items-center gap-1.5 text-xs text-muted-foreground font-mono">
            <span className="text-foreground font-medium">{t("page.tools.dashboard.tools_label")}</span>
            {activeTool && (
              <>
                <span>/</span>
                <span className="text-foreground">{t(activeTool.nameKey)}</span>
              </>
            )}
          </div>
        </header>

        <main className="flex-1 min-h-0 overflow-hidden">
          {activeTool && activeTool.component ? (
            <div className="h-full min-h-0">{activeTool.component()}</div>
          ) : (
            <div className="max-w-3xl mx-auto space-y-6 overflow-y-auto h-full">
              <div>
                <h1 className="text-xl font-semibold text-foreground text-balance">
                  {t("page.tools.dashboard.empty_title")}
                </h1>
                <p className="text-sm text-muted-foreground mt-1 leading-relaxed">
                  {t("page.tools.dashboard.empty_description")}
                </p>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {toolPages.map((tool, index) => {
                  const isExternal = !!tool.path;
                  const toolName = t(tool.nameKey);

                  if (isExternal) {
                    return (
                      <Link key={tool.nameKey} to={tool.path}>
                        <div className="group flex items-center justify-between p-4 rounded-lg border border-border bg-card hover:border-accent/40 hover:bg-card/80 transition-all cursor-pointer">
                          <div className="flex items-center gap-3">
                            <div className="flex h-9 w-9 items-center justify-center rounded-md bg-secondary text-sm font-bold font-mono text-muted-foreground">
                              {tool.initials}
                            </div>
                            <div>
                              <p className="text-sm font-medium text-foreground">{toolName}</p>
                              <p className="text-xs text-muted-foreground mt-0.5">
                                {t("page.tools.dashboard.external_page")}
                              </p>
                            </div>
                          </div>
                          <ExternalLink className="h-4 w-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                        </div>
                      </Link>
                    );
                  }

                  return (
                    <button
                      key={tool.nameKey}
                      onClick={() => setActiveIndex(index)}
                      className="group flex items-center justify-between p-4 rounded-lg border border-border bg-card hover:border-accent/40 hover:bg-card/80 transition-all text-left"
                    >
                      <div className="flex items-center gap-3">
                        <div className="flex h-9 w-9 items-center justify-center rounded-md bg-secondary text-sm font-bold font-mono text-muted-foreground group-hover:bg-accent/20 group-hover:text-accent transition-colors">
                          {tool.initials}
                        </div>
                        <div>
                          <p className="text-sm font-medium text-foreground">{toolName}</p>
                          <p className="text-xs text-muted-foreground mt-0.5">
                            {t("page.tools.dashboard.inline_tool")}
                          </p>
                        </div>
                      </div>
                      <ChevronRight className="h-4 w-4 text-muted-foreground opacity-0 group-hover:opacity-100 group-hover:text-accent transition-all" />
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
