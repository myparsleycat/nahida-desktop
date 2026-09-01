import type { ModFixerAction } from "@shared/types";

export function getModFixerAvailability(importer: string | null) {
    const normalized = importer?.toUpperCase() ?? null;
    const showWuwaFixer = normalized === null || normalized === "WWMI";
    const showZZMIFixer = normalized === "ZZMI";
    const modFixer: ModFixerAction | null = showZZMIFixer
        ? { id: "zzmi", label: "ZZMI Mod Fixer" }
        : showWuwaFixer
          ? { id: "wuwa", label: "Wuwa Mod Fixer" }
          : null;

    return { showWuwaFixer, showZZMIFixer, modFixer };
}
