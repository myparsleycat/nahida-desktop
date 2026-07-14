import assert from "node:assert/strict";

import { describe, it } from "vitest";

import { isDisabledBisectPath } from "./mod-bisect.ts";

describe("isDisabledBisectPath", () => {
    it("excludes every INI filename beginning with disabled", () => {
        assert.equal(isDisabledBisectPath("disabled.ini"), true);
        assert.equal(isDisabledBisectPath("DISABLEDfoo.ini"), true);
        assert.equal(isDisabledBisectPath("nested/DisabledBackup.ini"), true);
    });

    it("excludes files in disabled folders with supported separators", () => {
        assert.equal(isDisabledBisectPath("DISABLED Mod/mod.ini"), true);
        assert.equal(isDisabledBisectPath("nested/disabled_Mod/mod.ini"), true);
    });

    it("keeps enabled INIs and folders whose names merely start similarly", () => {
        assert.equal(isDisabledBisectPath("enabled.ini"), false);
        assert.equal(isDisabledBisectPath("disabledMod/mod.ini"), false);
    });
});
