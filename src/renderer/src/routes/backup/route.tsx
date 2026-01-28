import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/backup")({
  component: RouteComponent,
});

function RouteComponent() {
  return (
    <>
      <Titlebar title={{ text: "백업", position: "center" }} />
      <Outlet />
    </>
  );
}
