import type { ModFixerAction } from "@shared/types";

export function getModFixerAvailability(importer: string | null, t: (key: string) => string) {
    const normalized = importer?.toUpperCase() ?? null;
    const showWuwaFixer = normalized === null || normalized === "WWMI";
    const showZZMIFixer = normalized === "ZZMI";
    const modFixer: ModFixerAction | null = showZZMIFixer
        ? { id: "zzmi", label: t("page.mod.character-sidebar.zzmi-mod-fixer") }
        : showWuwaFixer
          ? { id: "wuwa", label: t("page.mod.character-sidebar.wuwa-mod-fixer") }
          : null;

    return { showWuwaFixer, showZZMIFixer, modFixer };
}
