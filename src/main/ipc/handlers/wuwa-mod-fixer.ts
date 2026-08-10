import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerWuwaModFixerHandlers(d: NahidaDesktop) {
    rh("wuwaFixer:getRateStatus", () => d.service.modTools.wuwaModFixer.getRateStatus());
    rh("wuwaFixer:getStatus", (importer = null) =>
        d.service.modTools.wuwaModFixer.getStatus(importer),
    );
    rh("wuwaFixer:prepareRun", (importer = null) =>
        d.service.modTools.wuwaModFixer.prepareRun(importer),
    );
    rh("wuwaFixer:installOrUpdate", () => d.service.modTools.wuwaModFixer.installOrUpdate());
    rh("wuwaFixer:run", (modPath, options) =>
        d.service.modTools.wuwaModFixer.run(modPath, options),
    );
    rh("wuwaFixer:scanBackups", (modPath) => d.service.modTools.wuwaModFixer.scanBackups(modPath));
    rh("wuwaFixer:getBackupSize", (modPath) =>
        d.service.modTools.wuwaModFixer.getBackupSize(modPath),
    );
    rh("wuwaFixer:rollbackToGroup", (modPath, groupKey) =>
        d.service.modTools.wuwaModFixer.rollbackToGroup(modPath, groupKey),
    );
    rh("wuwaFixer:cleanBackups", (modPath) =>
        d.service.modTools.wuwaModFixer.cleanBackups(modPath),
    );
}
