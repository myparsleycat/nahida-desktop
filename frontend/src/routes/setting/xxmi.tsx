import { XXMI } from "@bindings/xxmi";
import { XXMIDllVersion } from "@renderer/components/setting/xxmi/xxmi-dll-version";
import { XXMIImporters } from "@renderer/components/setting/xxmi/xxmi-importers";
import { XXMIPath } from "@renderer/components/setting/xxmi/xxmi-path";
import { Separator } from "@renderer/components/ui/separator";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/xxmi")({
  component: RouteComponent,
});

export type XXMIData = Awaited<ReturnType<typeof XXMI.GetXXMIData>>;

function RouteComponent() {
  return <XXMIRouteContent />;
}

function XXMIRouteContent() {
  const { data: xxmiData, refetch } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => XXMI.GetXXMIData(),
  });

  return (
    <main className="mx-auto flex w-full flex-1 flex-col space-y-6 p-4 select-none">
      <XXMIPath xxmiData={xxmiData} refetch={refetch} />
      <Separator />
      <XXMIDllVersion xxmiData={xxmiData} refetch={refetch} />
      {xxmiData?.xxmiConfig && (
        <>
          <Separator />
          <XXMIImporters xxmiData={xxmiData} />
        </>
      )}
    </main>
  );
}
