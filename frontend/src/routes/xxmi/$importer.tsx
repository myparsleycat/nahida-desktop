import { XXMIImporterSettings } from "@renderer/components/xxmi/xxmi-importer-settings";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/xxmi/$importer")({ component: RouteComponent });

function RouteComponent() {
  const { importer } = Route.useParams();
  // The route instance is reused across importers, so a key keeps one importer's draft from leaking into another.
  return <XXMIImporterSettings key={importer} importer={importer} />;
}
