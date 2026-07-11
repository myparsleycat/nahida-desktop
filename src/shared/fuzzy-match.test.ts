import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { findBestFuzzyMatch, rankFuzzyMatches } from "./fuzzy-match.ts";

const candidates = ["alice", "anby", "aria"];

void describe("fuzzy match", () => {
    void it("matches leading CamelCase tokens", () => {
        assert.equal(findBestFuzzyMatch(candidates, "AriaRaceQueen")?.value, "aria");
    });

    void it("matches separator tokens", () => {
        assert.equal(findBestFuzzyMatch(candidates, "aria_race_queen")?.value, "aria");
    });

    void it("matches normalized prefixes", () => {
        assert.equal(findBestFuzzyMatch(candidates, "ariaracequeen")?.value, "aria");
    });

    void it("tolerates token typos", () => {
        assert.equal(findBestFuzzyMatch(candidates, "AiraRaceQueen")?.value, "aria");
    });

    void it("matches anby download names", () => {
        assert.equal(findBestFuzzyMatch(candidates, "AnbyDefault")?.value, "anby");
    });

    void it("matches alice download names", () => {
        assert.equal(findBestFuzzyMatch(candidates, "AliceSwimsuit")?.value, "alice");
    });

    void it("requires exact tokens for short candidates", () => {
        assert.equal(findBestFuzzyMatch(["A"], "AliceSwimsuit", { threshold: 0.85 }), null);
        assert.equal(findBestFuzzyMatch(["an"], "AnbyDefault", { threshold: 0.85 }), null);
        assert.equal(findBestFuzzyMatch(["ari"], "AriaRaceQueen", { threshold: 0.85 }), null);
        assert.equal(findBestFuzzyMatch(["A"], "A_Default", { threshold: 0.85 })?.value, "A");
    });

    void it("rejects unrelated single-character substitutions", () => {
        assert.equal(findBestFuzzyMatch(["Lull"], "full", { threshold: 0.85 }), null);
    });

    void it("allows single edits after a stable prefix", () => {
        assert.equal(
            findBestFuzzyMatch(["alice"], "AlxceDefault", { threshold: 0.85 })?.value,
            "alice",
        );
    });

    void it("returns null when the best score is below threshold", () => {
        assert.equal(findBestFuzzyMatch(candidates, "zzz", { threshold: 0.95 }), null);
    });

    void it("handles empty candidates", () => {
        assert.deepEqual(rankFuzzyMatches([], "AriaRaceQueen"), []);
        assert.equal(findBestFuzzyMatch([], "AriaRaceQueen"), null);
    });

    void it("handles empty input safely", () => {
        assert.doesNotThrow(() => rankFuzzyMatches(candidates, ""));
        assert.equal(findBestFuzzyMatch(candidates, "", { threshold: 0.1 }), null);
    });
});
