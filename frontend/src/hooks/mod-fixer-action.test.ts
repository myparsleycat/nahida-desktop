import { describe, expect, it } from "vitest";

import { getModFixerAvailability } from "./mod-fixer-action";

const labels: Record<string, string> = {
    "page.mod.character-sidebar.zzmi-mod-fixer": "Localized ZZMI Mod Fixer",
    "page.mod.character-sidebar.wuwa-mod-fixer": "Localized Wuwa Mod Fixer",
};

const t = (key: string) => labels[key] ?? key;

describe("getModFixerAvailability", () => {
    it.each([
        ["WWMI", true, false, "wuwa"],
        ["wwmi", true, false, "wuwa"],
        ["ZZMI", false, true, "zzmi"],
        ["zzmi", false, true, "zzmi"],
        [null, true, false, "wuwa"],
        ["GIMI", false, false, null],
    ] as const)(
        "maps importer %s to the expected fixer",
        (importer, showWuwaFixer, showZZMIFixer, fixerId) => {
            expect(getModFixerAvailability(importer, t)).toEqual({
                showWuwaFixer,
                showZZMIFixer,
                modFixer: fixerId
                    ? {
                          id: fixerId,
                          label:
                              fixerId === "zzmi"
                                  ? labels["page.mod.character-sidebar.zzmi-mod-fixer"]
                                  : labels["page.mod.character-sidebar.wuwa-mod-fixer"],
                      }
                    : null,
            });
        },
    );
});
