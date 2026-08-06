import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/test")({
  component: RouteComponent,
});

function RouteComponent() {
  return <Titlebar title={{ text: "Debug", position: "center" }} />;
}
