import { XXMIImporters } from "@renderer/components/setting/xxmi/xxmi-importers";
import { XXMIPath } from "@renderer/components/setting/xxmi/xxmi-path";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/xxmi")({
  component: RouteComponent,
});

import type { IpcHandlers } from "@shared/types";

export type XXMIData = Awaited<ReturnType<IpcHandlers["xxmi:getXXMIData"]>>;

function RouteComponent() {
  return <XXMIRouteContent />;
}

function XXMIRouteContent() {
  const { data: xxmiData, refetch } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => window.api.invoke("xxmi:getXXMIData"),
  });

  return (
    <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none">
      <XXMIPath xxmiData={xxmiData} refetch={refetch} />
      <XXMIImporters xxmiData={xxmiData} />
    </main>
  );
}
