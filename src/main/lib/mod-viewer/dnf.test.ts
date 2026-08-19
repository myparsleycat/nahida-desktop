import assert from "node:assert/strict";

import { dnfSatisfied } from "@shared/mod-viewer/eval";
import { describe, it } from "vitest";

import {
    DNF_FALSE,
    DNF_TRUE,
    dnfAnd,
    dnfCovers,
    dnfNot,
    dnfOr,
    isUnconstrained,
    normalizeDnf,
    parseConditionDnf,
} from "./dnf";

describe("dnf logic", () => {
    it("distinguishes DNF_FALSE from DNF_TRUE and unconstrained conditions", () => {
        assert.equal(isUnconstrained(DNF_TRUE), true);
        assert.equal(isUnconstrained(DNF_FALSE), false);
        assert.equal(isUnconstrained([[{ var: "swap", value: "1", negate: false }]]), false);

        assert.equal(dnfSatisfied(DNF_TRUE, {}), true);
        assert.equal(dnfSatisfied(DNF_FALSE, {}), false);
        assert.equal(dnfSatisfied(undefined, {}), true);
    });

    it("evaluates dnfNot correctly for true, false, and clauses", () => {
        assert.deepEqual(dnfNot(DNF_TRUE), DNF_FALSE);
        assert.deepEqual(dnfNot(DNF_FALSE), DNF_TRUE);

        const clause = [[{ var: "outfit", value: "0", negate: false }]];
        const negated = dnfNot(clause);
        assert.deepEqual(negated, [[{ var: "outfit", value: "0", negate: true }]]);
        assert.deepEqual(dnfNot(negated), clause);
    });

    it("evaluates dnfAnd preserving DNF_FALSE", () => {
        const cond = [[{ var: "outfit", value: "1", negate: false }]];
        assert.deepEqual(dnfAnd(cond, DNF_FALSE), DNF_FALSE);
        assert.deepEqual(dnfAnd(DNF_FALSE, cond), DNF_FALSE);
        assert.deepEqual(dnfAnd(cond, DNF_TRUE), cond);
        assert.deepEqual(dnfAnd(DNF_TRUE, cond), cond);
    });

    it("evaluates dnfOr with DNF_FALSE and DNF_TRUE", () => {
        const cond = [[{ var: "outfit", value: "1", negate: false }]];
        assert.deepEqual(dnfOr(cond, DNF_FALSE), cond);
        assert.deepEqual(dnfOr(DNF_FALSE, cond), cond);
        assert.deepEqual(dnfOr(cond, DNF_TRUE), DNF_TRUE);
        assert.deepEqual(dnfOr(DNF_TRUE, cond), DNF_TRUE);
    });

    it("normalizes DNF without turning DNF_FALSE into unconstrained true", () => {
        assert.deepEqual(normalizeDnf(DNF_FALSE, ["outfit"]), DNF_FALSE);
        assert.deepEqual(normalizeDnf(DNF_TRUE, ["outfit"]), DNF_TRUE);

        const untracked = [[{ var: "internal", value: "1", negate: false }]];
        assert.deepEqual(normalizeDnf(untracked, ["outfit"]), DNF_TRUE);

        const tracked = [[{ var: "outfit", value: "1", negate: false }]];
        assert.deepEqual(normalizeDnf(tracked, ["outfit"]), tracked);
    });

    it("handles comparisons expanding to DNF_FALSE", () => {
        const dnf = parseConditionDnf("$top > 5", {}, { top: ["0", "1", "2"] });
        assert.deepEqual(dnf, DNF_FALSE);
        assert.equal(dnfSatisfied(dnf, { top: "0" }), false);
    });

    it("parses constant 0 as DNF_FALSE and 1 as unconstrained true", () => {
        assert.deepEqual(parseConditionDnf("0", {}), DNF_FALSE);
        assert.deepEqual(parseConditionDnf("1", {}), DNF_TRUE);
        assert.deepEqual(dnfNot(parseConditionDnf("0", {})), DNF_TRUE);
    });

    it("covers conditions correctly", () => {
        assert.equal(dnfCovers(DNF_TRUE, DNF_TRUE), true);
        assert.equal(dnfCovers(DNF_TRUE, DNF_FALSE), false);
        assert.equal(dnfCovers(DNF_FALSE, DNF_TRUE), true);
        assert.equal(dnfCovers(DNF_FALSE, DNF_FALSE), false);
    });
});
