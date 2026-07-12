import assert from "node:assert/strict";

import { describe, it } from "vitest";

import {
    DISABLED_PREFIX_REGEX,
    manualSubGroupSegmentMatches,
    normalizeRelativePath,
    restoreDisabledPrefix,
    stripDisabledPrefix,
} from "./path-utils.ts";

describe("DISABLED_PREFIX_REGEX", () => {
    it("matches space separator", () => {
        assert.equal(DISABLED_PREFIX_REGEX.test("DISABLED Foo"), true);
    });

    it("matches underscore separator", () => {
        assert.equal(DISABLED_PREFIX_REGEX.test("DISABLED_Foo"), true);
    });

    it("matches case-insensitively", () => {
        assert.equal(DISABLED_PREFIX_REGEX.test("disabled Foo"), true);
        assert.equal(DISABLED_PREFIX_REGEX.test("Disabled_Foo"), true);
    });

    it("does not match without separator", () => {
        assert.equal(DISABLED_PREFIX_REGEX.test("DisableFoo"), false);
        assert.equal(DISABLED_PREFIX_REGEX.test("Disable_Foo"), false);
    });

    it("does not match empty string", () => {
        assert.equal(DISABLED_PREFIX_REGEX.test(""), false);
    });

    it("does not match 'disabled' repeated without separator", () => {
        assert.equal(DISABLED_PREFIX_REGEX.test("disableddisableddisabledFoo"), false);
    });
});

describe("stripDisabledPrefix", () => {
    it("strips space prefix", () => {
        assert.equal(stripDisabledPrefix("DISABLED Foo"), "Foo");
    });

    it("strips underscore prefix", () => {
        assert.equal(stripDisabledPrefix("DISABLED_Foo"), "Foo");
        assert.equal(stripDisabledPrefix("disabled_My_Mod"), "My_Mod");
    });

    it("strips case-insensitively", () => {
        assert.equal(stripDisabledPrefix("disabled Foo"), "Foo");
        assert.equal(stripDisabledPrefix("Disabled_Foo"), "Foo");
    });

    it("strips trailing whitespace after name", () => {
        assert.equal(stripDisabledPrefix("DISABLED Foo  "), "Foo");
    });

    it("strips extra whitespace between prefix and name", () => {
        assert.equal(stripDisabledPrefix("DISABLED  Foo"), "Foo");
    });

    it("returns name as-is when no prefix", () => {
        assert.equal(stripDisabledPrefix("My Mod"), "My Mod");
        assert.equal(stripDisabledPrefix("DisableFoo"), "DisableFoo");
    });

    it("handles multibyte names without prefix", () => {
        assert.equal(stripDisabledPrefix("仪玄-黑珍珠"), "仪玄-黑珍珠");
    });

    it("handles multibyte names with prefix", () => {
        assert.equal(stripDisabledPrefix("DISABLED 仪玄"), "仪玄");
    });

    it("does not strip repeated 'disabled' without separator", () => {
        assert.equal(
            stripDisabledPrefix("disableddisableddisabledFoo"),
            "disableddisableddisabledFoo",
        );
    });

    it("strips only the first prefix with separator repetition (double)", () => {
        assert.equal(stripDisabledPrefix("disabled_disabled_Foo"), "disabled_Foo");
        assert.equal(stripDisabledPrefix("disabled disabled Foo"), "disabled Foo");
    });

    it("strips only the first prefix with separator repetition (triple)", () => {
        assert.equal(
            stripDisabledPrefix("disabled_disabled_disabled_foo"),
            "disabled_disabled_foo",
        );
        assert.equal(
            stripDisabledPrefix("disabled disabled disabled foo"),
            "disabled disabled foo",
        );
    });
});

describe("restoreDisabledPrefix", () => {
    it("preserves space separator from source", () => {
        assert.equal(restoreDisabledPrefix("DISABLED OldName", "NewName"), "DISABLED NewName");
    });

    it("preserves underscore separator from source", () => {
        assert.equal(restoreDisabledPrefix("DISABLED_OldName", "NewName"), "DISABLED_NewName");
    });

    it("preserves lowercase casing from source", () => {
        assert.equal(restoreDisabledPrefix("disabled OldName", "NewName"), "disabled NewName");
    });

    it("does not add prefix when source has none", () => {
        assert.equal(restoreDisabledPrefix("OldName", "NewName"), "NewName");
    });
});

describe("normalizeRelativePath", () => {
    it("strips prefix per segment and lowercases", () => {
        assert.equal(normalizeRelativePath("DISABLED Foo/Bar"), "foo/bar");
    });

    it("strips underscore prefix per segment", () => {
        assert.equal(normalizeRelativePath("DISABLED_Foo/Bar"), "foo/bar");
    });

    it("handles backslashes", () => {
        assert.equal(normalizeRelativePath("DISABLED Foo\\Bar"), "foo/bar");
    });

    it("handles mixed separators", () => {
        assert.equal(normalizeRelativePath("Foo\\DISABLED Bar/Baz"), "foo/bar/baz");
    });

    it("produces same key for enabled and disabled variants", () => {
        assert.equal(normalizeRelativePath("DISABLED MyMod"), normalizeRelativePath("MyMod"));
    });
});

describe("manualSubGroupSegmentMatches", () => {
    it("matches exact entry name", () => {
        assert.equal(manualSubGroupSegmentMatches("Foo", "foo"), true);
    });

    it("matches by stripping disabled prefix", () => {
        assert.equal(manualSubGroupSegmentMatches("DISABLED Foo", "foo"), true);
        assert.equal(manualSubGroupSegmentMatches("DISABLED_Foo", "foo"), true);
    });

    it("does not match different names", () => {
        assert.equal(manualSubGroupSegmentMatches("DISABLED Foo", "bar"), false);
    });

    it("does not match when stored segment has prefix", () => {
        assert.equal(manualSubGroupSegmentMatches("Foo", "disabled foo"), false);
    });
});
