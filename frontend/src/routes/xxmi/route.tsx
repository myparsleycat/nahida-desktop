import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/xxmi")({ component: RouteComponent });

function RouteComponent() {
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-background text-foreground">
      <header className="flex h-10 shrink-0 items-center border-b border-border px-4">
        <span className="font-mono text-xs font-medium text-foreground">XXMI</span>
      </header>

      <div className="min-h-0 flex-1 scrollbar-gutter-stable overflow-y-auto">
        <div className="mx-auto min-h-full w-full max-w-xl">
          <Outlet />
        </div>
      </div>
    </div>
  );
}
