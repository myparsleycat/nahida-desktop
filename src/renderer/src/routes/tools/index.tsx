import { D3D11Builder } from "@renderer/components/tools/d3d11-builder";
import { cn } from "@renderer/lib/utils";
import { createFileRoute, Link } from "@tanstack/react-router";

export const Route = createFileRoute("/tools/")({
  component: RouteComponent,
});

function RouteComponent() {
  const toolPages = [
    {
      name: "D3D11 Builder",
      component: () => <D3D11Builder />,
    },
    {
      name: "Fix Tool Manager",
      path: "/tools/fix-tool-manger",
    },
  ];

  return (
    <div className="p-4 grid grid-cols-1 md:grid-cols-2 grid-rows-2 gap-4 h-full">
      {toolPages.map((page) => {
        const itemClassName = cn(
          "p-4 border rounded-md bg-card transition-colors flex justify-center items-center size-full",
        );

        if (page.component) {
          return (
            <div key={page.name} className={cn(itemClassName)}>
              {page.component()}
            </div>
          );
        }

        if (page.path) {
          return (
            <Link
              key={page.path}
              to={page.path}
              className={cn(itemClassName, "flex justify-center items-center")}
            >
              <h2 className="text-xl font-semibold">{page.name}</h2>
            </Link>
          );
        }

        return null;
      })}
    </div>
  );
}
