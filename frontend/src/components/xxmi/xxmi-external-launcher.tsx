import { XXMI } from "@bindings/xxmi";
import { Separator } from "@renderer/components/ui/separator";
import { XXMIDllVersion } from "@renderer/components/xxmi/xxmi-dll-version";
import { XXMIImporters } from "@renderer/components/xxmi/xxmi-importers";
import { XXMIPackageVersion } from "@renderer/components/xxmi/xxmi-package-version";
import { XXMIPath } from "@renderer/components/xxmi/xxmi-path";
import { useQuery } from "@tanstack/react-query";

export function XXMIExternalLauncher() {
  const { data: xxmiData, refetch } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => XXMI.GetXXMIData(),
  });

  return (
    <>
      <XXMIPath xxmiData={xxmiData} refetch={refetch} />
      <Separator />
      <XXMIDllVersion xxmiData={xxmiData} refetch={refetch} />
      {xxmiData?.xxmiPath && (
        <>
          {(xxmiData.enabledImporters?.length ?? 0) > 0 && (
            <>
              <Separator />
              <XXMIPackageVersion xxmiData={xxmiData} refetch={refetch} />
            </>
          )}
          <Separator />
          <XXMIImporters xxmiData={xxmiData} />
        </>
      )}
    </>
  );
}
