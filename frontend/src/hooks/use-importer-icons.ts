import { XXMI } from "@bindings/xxmi";
import { useQuery } from "@tanstack/react-query";

export const importerIconsQueryKey = ["xxmi:importer-icons"];

export function useImporterIcons() {
    return useQuery({
        queryKey: importerIconsQueryKey,
        queryFn: XXMI.GetImporterIcons,
        staleTime: Infinity,
    });
}
