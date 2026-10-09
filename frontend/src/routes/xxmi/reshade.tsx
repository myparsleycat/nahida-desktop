import { ReShadeSettings } from "@renderer/components/xxmi/reshade-settings";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/xxmi/reshade")({ component: ReShadeSettings });
