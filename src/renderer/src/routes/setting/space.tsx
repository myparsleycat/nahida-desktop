import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/space")({
  component: RouteComponent,
});

function RouteComponent() {
  return <div>Hello "/setting/folder"!</div>;
}
