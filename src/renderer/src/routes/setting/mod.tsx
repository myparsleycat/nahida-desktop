import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/mod")({
  component: RouteComponent,
});

function RouteComponent() {
  return <div>Hello "/setting/mod"!</div>;
}
