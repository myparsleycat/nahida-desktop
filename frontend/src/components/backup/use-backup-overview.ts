import { Backup } from "@bindings/backup";
import { useQuery } from "@tanstack/react-query";

import { backupOverviewKey } from "./queries";

export function useBackupOverview() {
    return useQuery({ queryKey: backupOverviewKey, queryFn: () => Backup.GetOverview() });
}
