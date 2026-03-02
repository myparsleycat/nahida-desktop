import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/backup")({
  component: RouteComponent,
});

function RouteComponent() {
  const { Titlebar } = useTitlebar();

  return (
    <>
      <Titlebar title={{ text: "백업", position: "center" }} />
      <Outlet />
    </>
  );
}
