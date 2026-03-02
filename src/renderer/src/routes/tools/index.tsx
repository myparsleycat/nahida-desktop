import ToolsPage from "@renderer/components/tools/tools-dashboard";
import { cn } from "@renderer/lib/utils";
import { createFileRoute, Link } from "@tanstack/react-router";

export const Route = createFileRoute("/tools/")({
  component: ToolsPage,
});
