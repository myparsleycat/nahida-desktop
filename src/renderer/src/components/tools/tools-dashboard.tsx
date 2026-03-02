import D3D11Builder from "@renderer/components/tools/d3d11-builder";
import TogglePersistence from "@renderer/components/tools/toggle-persistence";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { cn } from "@renderer/lib/utils";
import { Link } from "@tanstack/react-router";
import { ChevronRight, Wrench, ExternalLink, Menu, X } from "lucide-react";
import { useState } from "react";
import FixToolManager from "./fix-tool-manger";

type ToolPage =
  | {
      name: string;
      component: () => React.ReactNode;
      path?: never;
    }
  | {
      name: string;
      path: string;
      component?: never;
    };

const toolPages: ToolPage[] = [
  {
    name: "D3D11 Builder",
    component: () => <D3D11Builder />,
  },
  {
    name: "Toggle Persistence",
    component: () => <TogglePersistence />,
  },
  {
    name: "Fix Tool Manager",
    component: () => <FixToolManager />,
  },
];

function getToolInitials(name: string) {
  return name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

export default function ToolsPage() {
  const { screenHeight } = useTitlebar();
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
            Mod Tools
          </span>
        </div>

        <nav className="flex-1 overflow-y-auto py-3 px-2">
          <p className="px-2 mb-2 text-[10px] font-medium uppercase tracking-widest text-muted-foreground">
            Tools
          </p>
          <ul className="space-y-0.5">
            {toolPages.map((tool, index) => {
              const isActive = activeIndex === index;
              const isExternal = !!tool.path;

              if (isExternal) {
                return (
                  <li key={tool.name}>
                    <Link
                      to={tool.path}
                      className="flex items-center justify-between gap-2 rounded-md px-2 py-2 text-sm text-sidebar-foreground hover:bg-sidebar-accent transition-colors group"
                    >
                      <div className="flex items-center gap-2.5">
                        <span className="flex h-5 w-5 items-center justify-center rounded bg-secondary text-[9px] font-bold text-muted-foreground font-mono">
                          {getToolInitials(tool.name)}
                        </span>
                        <span className="text-xs">{tool.name}</span>
                      </div>
                      <ExternalLink className="h-3 w-3 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                    </Link>
                  </li>
                );
              }

              return (
                <li key={tool.name}>
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
                        {getToolInitials(tool.name)}
                      </span>
                      <span className="text-xs">{tool.name}</span>
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
            aria-label="Toggle sidebar"
          >
            {sidebarOpen ? (
              <X className="h-4 w-4 text-muted-foreground" />
            ) : (
              <Menu className="h-4 w-4 text-muted-foreground" />
            )}
          </button>

          <div className="flex items-center gap-1.5 text-xs text-muted-foreground font-mono">
            <span className="text-foreground font-medium">Tools</span>
            {activeTool && (
              <>
                <span>/</span>
                <span className="text-foreground">{activeTool.name}</span>
              </>
            )}
          </div>
        </header>

        <main className="flex-1 min-h-0 overflow-hidden p-4">
          {activeTool && activeTool.component ? (
            <div className="h-full min-h-0 max-w-2xl mx-auto">{activeTool.component()}</div>
          ) : (
            <div className="max-w-3xl mx-auto space-y-6 overflow-y-auto h-full">
              <div>
                <h1 className="text-xl font-semibold text-foreground text-balance">
                  Developer Tools
                </h1>
                <p className="text-sm text-muted-foreground mt-1 leading-relaxed">
                  Select a tool from the sidebar or pick one below to get started.
                </p>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {toolPages.map((tool, index) => {
                  const isExternal = !!tool.path;

                  if (isExternal) {
                    return (
                      <Link key={tool.name} to={tool.path}>
                        <div className="group flex items-center justify-between p-4 rounded-lg border border-border bg-card hover:border-accent/40 hover:bg-card/80 transition-all cursor-pointer">
                          <div className="flex items-center gap-3">
                            <div className="flex h-9 w-9 items-center justify-center rounded-md bg-secondary text-sm font-bold font-mono text-muted-foreground">
                              {getToolInitials(tool.name)}
                            </div>
                            <div>
                              <p className="text-sm font-medium text-foreground">{tool.name}</p>
                              <p className="text-xs text-muted-foreground mt-0.5">External page</p>
                            </div>
                          </div>
                          <ExternalLink className="h-4 w-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                        </div>
                      </Link>
                    );
                  }

                  return (
                    <button
                      key={tool.name}
                      onClick={() => setActiveIndex(index)}
                      className="group flex items-center justify-between p-4 rounded-lg border border-border bg-card hover:border-accent/40 hover:bg-card/80 transition-all text-left"
                    >
                      <div className="flex items-center gap-3">
                        <div className="flex h-9 w-9 items-center justify-center rounded-md bg-secondary text-sm font-bold font-mono text-muted-foreground group-hover:bg-accent/20 group-hover:text-accent transition-colors">
                          {getToolInitials(tool.name)}
                        </div>
                        <div>
                          <p className="text-sm font-medium text-foreground">{tool.name}</p>
                          <p className="text-xs text-muted-foreground mt-0.5">Inline tool</p>
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
