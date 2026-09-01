import { MenuMakerPage } from "@renderer/components/tools/menu-maker/menu-maker-page";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/tools/menu-maker")({
  validateSearch: (search: Record<string, unknown>) => ({
    path: typeof search.path === "string" ? search.path : "",
    name: typeof search.name === "string" ? search.name : "",
    ini: typeof search.ini === "string" ? search.ini : "",
  }),
  component: RouteComponent,
});

function RouteComponent() {
  const { path, name, ini } = Route.useSearch();
  return <MenuMakerPage path={path} name={name} ini={ini} />;
}
