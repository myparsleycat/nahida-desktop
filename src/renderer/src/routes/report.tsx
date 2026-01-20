import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/report")({
  component: RouteComponent,
});

function RouteComponent() {
  return (
    <div>
      <Titlebar title={{ text: "문제 신고", position: "center" }} />
    </div>
  );
}
