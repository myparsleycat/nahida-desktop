import { ModelViewerWindow } from "@renderer/components/tools/model-viewer/model-viewer-window";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/model-viewer-window")({
  validateSearch: (search: Record<string, unknown>) => ({
    path: typeof search.path === "string" ? search.path : "",
  }),
  component: RouteComponent,
});

function RouteComponent() {
  const { path } = Route.useSearch();
  return <ModelViewerWindow key={path} path={path} />;
}
