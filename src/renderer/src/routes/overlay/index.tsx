import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/overlay/")({
  component: RouteComponent,
});

function RouteComponent() {
  return (
    <div>
      <div>
        <img src="/assets/logo.png" />
      </div>
    </div>
  );
}
