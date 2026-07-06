import ToolsPage from "@renderer/components/tools/tools-dashboard";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/tools/")({
  component: ToolsPage,
});
