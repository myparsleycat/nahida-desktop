import { ModelViewerPage } from "@renderer/components/tools/model-viewer/model-viewer-page";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/tools/model-viewer")({
  validateSearch: (search: Record<string, unknown>) => ({
    path: typeof search.path === "string" ? search.path : "",
    name: typeof search.name === "string" ? search.name : "",
    manifestPath: typeof search.manifestPath === "string" ? search.manifestPath : "",
    artifactRoot: typeof search.artifactRoot === "string" ? search.artifactRoot : "",
  }),
  component: RouteComponent,
});

function RouteComponent() {
  const { path, name, manifestPath, artifactRoot } = Route.useSearch();

  return (
    <ModelViewerPage
      path={path}
      name={name}
      manifestPath={manifestPath}
      artifactRoot={artifactRoot}
    />
  );
}
