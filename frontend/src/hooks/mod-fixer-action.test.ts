import { describe, expect, it } from "vitest";

import { getModFixerAvailability } from "./mod-fixer-action";

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
            expect(getModFixerAvailability(importer)).toEqual({
                showWuwaFixer,
                showZZMIFixer,
                modFixer: fixerId
                    ? {
                          id: fixerId,
                          label: fixerId === "zzmi" ? "ZZMI Mod Fixer" : "Wuwa Mod Fixer",
                      }
                    : null,
            });
        },
    );
});
