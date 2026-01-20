import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/sync")({
  component: RouteComponent,
});

function RouteComponent() {
  return <div>Hello "/setting/sync"!</div>;
}
