import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/net")({
  component: RouteComponent,
});

function RouteComponent() {
  return <div>Hello "/setting/network"!</div>;
}
