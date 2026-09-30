import { XXMIImporterSettings } from "@renderer/components/setting/xxmi-importer-settings";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/xxmi/$importer")({ component: RouteComponent });

function RouteComponent() {
  const { importer } = Route.useParams();
  return <XXMIImporterSettings importer={importer} />;
}
