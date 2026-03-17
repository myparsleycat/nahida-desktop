import { WindowsOnlyRoute } from "@renderer/components/windows-only-route";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/tools")({
  component: RouteComponent,
});

function RouteComponent() {
  return (
    <WindowsOnlyRoute fallbackTo="/transfer">
      <ToolsRouteContent />
    </WindowsOnlyRoute>
  );
}

function ToolsRouteContent() {
  const { Titlebar } = useTitlebar();

  return (
    <>
      <Titlebar title={{ text: "도구", position: "center" }} />
      <Outlet />
    </>
  );
}
