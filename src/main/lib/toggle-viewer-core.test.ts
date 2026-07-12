import assert from "node:assert/strict";

import { describe, it } from "vitest";

import { parseIni } from "./toggle-viewer-core.ts";

describe("parseIni", () => {
    it("preserves a bare semicolon key value", () => {
        const sections = parseIni("[KeyOne]\nkey = ;\n");
        assert.equal(sections.length, 1);
        assert.equal(sections[0].entries.length, 1);
        assert.equal(sections[0].entries[0].key, "key");
        assert.equal(sections[0].entries[0].value, ";");
    });

    it("preserves a modifier + semicolon key value", () => {
        const sections = parseIni("[KeyTwo]\nkey = ctrl ;\n");
        assert.equal(sections[0].entries[0].value, "ctrl ;");
    });

    it("skips a leading-semicolon comment line", () => {
        const sections = parseIni("; full line comment\n[KeyThree]\nkey = x\n");
        assert.equal(sections.length, 1);
        assert.equal(sections[0].entries[0].value, "x");
    });

    it("preserves inline comment text in a value", () => {
        const sections = parseIni("[KeyFour]\nkey = 0 ; note\n");
        assert.equal(sections[0].entries[0].value, "0 ; note");
    });
});
