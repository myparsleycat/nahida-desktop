import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/tools")({
  component: RouteComponent,
});

function RouteComponent() {
  return <ToolsRouteContent />;
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
