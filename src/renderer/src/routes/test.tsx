import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/test")({
  component: RouteComponent,
});

function RouteComponent() {
  throw new Error("Intentional runtime error");

  // oxlint-disable-next-line no-unreachable
  return (
    <div>
      <Titlebar title={{ text: "test", position: "center" }} />
    </div>
  );
}
