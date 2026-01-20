import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/bak")({
  component: RouteComponent,
});

function RouteComponent() {
  return <div>Hello "/setting/backup"!</div>;
}
