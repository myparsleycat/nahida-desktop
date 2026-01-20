import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/noti")({
  component: RouteComponent,
});

function RouteComponent() {
  return <div>Hello "/setting/noti"!</div>;
}
