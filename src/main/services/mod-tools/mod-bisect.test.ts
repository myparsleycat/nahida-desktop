import assert from "node:assert/strict";
import path from "node:path";

import { describe, it } from "vitest";

import {
    BISECT_EXCLUDE_EMPTY,
    BISECT_EXCLUDE_OUTSIDE,
    BISECT_EXCLUDE_ROOT,
    isDisabledBisectPath,
    isExcludedBisectPath,
    toModRootRelativePath,
} from "./mod-bisect.ts";

const modRoot = path.resolve("/tmp/nhd-bisect-mods");

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

describe("toModRootRelativePath", () => {
    it("accepts relative and absolute paths inside the importer root", () => {
        assert.equal(toModRootRelativePath(modRoot, "KeepA/SomeMod"), "KeepA/SomeMod");
        assert.equal(
            toModRootRelativePath(modRoot, path.join(modRoot, "KeepA", "SomeMod")),
            "KeepA/SomeMod",
        );
    });

    it("rejects empty input, the importer root, and escaped paths", () => {
        assert.throws(() => toModRootRelativePath(modRoot, "  "), {
            message: BISECT_EXCLUDE_EMPTY,
        });
        assert.throws(() => toModRootRelativePath(modRoot, "."), { message: BISECT_EXCLUDE_ROOT });
        assert.throws(() => toModRootRelativePath(modRoot, modRoot), {
            message: BISECT_EXCLUDE_ROOT,
        });
        assert.throws(() => toModRootRelativePath(modRoot, path.join("..", "outside")), {
            message: BISECT_EXCLUDE_OUTSIDE,
        });
        assert.throws(
            () => toModRootRelativePath(modRoot, path.resolve(modRoot, "..", "outside")),
            { message: BISECT_EXCLUDE_OUTSIDE },
        );
    });
});

describe("isExcludedBisectPath", () => {
    it("excludes a folder and all descendant INIs", () => {
        const excludeAbs = path.join(modRoot, "KeepA");
        assert.equal(
            isExcludedBisectPath(path.join(modRoot, "KeepA", "mod.ini"), [excludeAbs]),
            true,
        );
        assert.equal(
            isExcludedBisectPath(path.join(modRoot, "KeepA", "nested", "a.ini"), [excludeAbs]),
            true,
        );
        assert.equal(
            isExcludedBisectPath(path.join(modRoot, "Other", "mod.ini"), [excludeAbs]),
            false,
        );
    });

    it("excludes a single INI without its siblings", () => {
        const excludeAbs = path.join(modRoot, "KeepA", "mod.ini");
        assert.equal(isExcludedBisectPath(excludeAbs, [excludeAbs]), true);
        assert.equal(
            isExcludedBisectPath(path.join(modRoot, "KeepA", "other.ini"), [excludeAbs]),
            false,
        );
    });
});
