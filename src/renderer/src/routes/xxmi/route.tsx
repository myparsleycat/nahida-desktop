import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/xxmi")({
  component: RouteComponent,
});

function RouteComponent() {
  return (
    <>
      <Titlebar title={{ text: "XXMI", position: "center" }} />
      <Outlet />
    </>
  );
}
