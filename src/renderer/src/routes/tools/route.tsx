import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/tools")({
  component: RouteComponent,
});

function RouteComponent() {
  return (
    <>
      <Titlebar title={{ text: "도구", position: "center" }} />
      <Outlet />
    </>
  );
}
