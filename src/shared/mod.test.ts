import assert from "node:assert/strict";

import { describe, it } from "vitest";

import { disabledPrefixString, stripDisabledPrefix } from "./mod.ts";

describe("disabledPrefixString", () => {
    it("returns DISABLED with trailing space for space style", () => {
        assert.equal(disabledPrefixString("space"), "DISABLED ");
    });

    it("returns DISABLED with trailing underscore for underscore style", () => {
        assert.equal(disabledPrefixString("underscore"), "DISABLED_");
    });

    it("produces a 9-character prefix for both styles", () => {
        assert.equal(disabledPrefixString("space").length, 9);
        assert.equal(disabledPrefixString("underscore").length, 9);
    });
});

describe("stripDisabledPrefix", () => {
    it("strips repeated space and underscore prefixes", () => {
        assert.equal(stripDisabledPrefix("DISABLED DISABLED Foo"), "Foo");
        assert.equal(stripDisabledPrefix("DISABLED_DISABLED_Foo"), "Foo");
        assert.equal(stripDisabledPrefix("disableddisabled Foo"), "Foo");
    });

    it("preserves disabled text without a final separator", () => {
        assert.equal(
            stripDisabledPrefix("disableddisableddisabledFoo"),
            "disableddisableddisabledFoo",
        );
    });
});
