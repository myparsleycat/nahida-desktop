import assert from "node:assert/strict";

import { describe, it } from "vitest";

import {
    fingerprintTogglePersistIni,
    parseTogglePersistProfile,
    TogglePersistLearner,
} from "./toggle-persist-learning";

const targetIniPath = "C:\\Mods\\Example\\example.ini";

describe("TogglePersistLearner", () => {
    it("emits an isolated change after the initial quiet window", () => {
        const learner = new TogglePersistLearner();
        learner.observe({ targetIniPath, varName: "Toggle", value: "1", revision: 1, at: 0 });

        assert.equal(learner.takeReady(targetIniPath, 2_999).updates.size, 0);
        assert.deepEqual([...learner.takeReady(targetIniPath, 3_000).updates], [["toggle", "1"]]);
    });

    it("coalesces a short user adjustment burst to its final value", () => {
        const learner = new TogglePersistLearner();
        ["0.1", "0.4", "0.8"].forEach((value, index) => {
            learner.observe({
                targetIniPath,
                varName: "Amount",
                value,
                revision: index + 1,
                at: index * 1_000,
            });
        });

        assert.equal(learner.takeReady(targetIniPath, 4_999).updates.size, 0);
        assert.deepEqual([...learner.takeReady(targetIniPath, 5_000).updates], [["amount", "0.8"]]);
    });

    it("suppresses a continuous numeric stream and discards its final value", () => {
        const learner = new TogglePersistLearner();
        const suppressed = new Set<string>();

        Array.from({ length: 10 }, (_, index) => index / 10).forEach((value, index) => {
            learner
                .observe({
                    targetIniPath,
                    varName: "Phase",
                    value: String(value),
                    revision: index + 1,
                    at: index * 2_000,
                })
                .newlySuppressed.forEach((name) => suppressed.add(name));
        });

        assert.deepEqual([...suppressed], ["Phase"]);
        assert.equal(learner.takeReady(targetIniPath, 60_000).updates.size, 0);
    });

    it("keeps learning across quiet-window flushes between periodic bursts", () => {
        const learner = new TogglePersistLearner();
        const suppressed = new Set<string>();
        const learned = new Set<string>();
        const updates: [string, string][] = [];

        Array.from({ length: 12 }, (_, index) => index).forEach((index) => {
            const at = index * 15_000;
            const result = learner.observe({
                targetIniPath,
                varName: "Phase",
                value: String(index / 12),
                revision: index + 1,
                at,
            });
            result.newlySuppressed.forEach((name) => suppressed.add(name));
            result.newlyLearned.forEach((variable) => learned.add(variable.name));
            updates.push(...learner.takeReady(targetIniPath, at + 10_000).updates);
        });

        assert.deepEqual([...suppressed], ["Phase"]);
        assert.deepEqual([...learned], ["Phase"]);
        assert.equal(updates.length, 7);
        assert.deepEqual(updates.at(-1), ["phase", "0.5"]);
    });

    it("detects a regular discrete cycle", () => {
        const learner = new TogglePersistLearner();
        const suppressed = new Set<string>();

        Array.from({ length: 10 }, (_, index) => (index % 2 === 0 ? "1" : "-1")).forEach(
            (value, index) => {
                learner
                    .observe({
                        targetIniPath,
                        varName: "Direction",
                        value,
                        revision: index + 1,
                        at: index * 4_000,
                    })
                    .newlySuppressed.forEach((name) => suppressed.add(name));
            },
        );

        assert.deepEqual([...suppressed], ["Direction"]);
    });

    it("learns a synchronized sparse cycle pair after it separates from the primary stream", () => {
        const learner = new TogglePersistLearner();
        const suppressed = new Set<string>();
        const learned = new Set<string>();

        Array.from({ length: 8 }, (_, index) => {
            const at = index * 3_000;
            const result = learner.observe({
                targetIniPath,
                varName: "Phase",
                value: String(index / 8),
                revision: index + 1,
                at,
            });
            result.newlySuppressed.forEach((name) => suppressed.add(name));
        });

        [
            { revision: 1, at: 0, value: "1" },
            { revision: 4, at: 9_000, value: "-1" },
            { revision: 9, at: 30_000, value: "1" },
            { revision: 10, at: 45_000, value: "-1" },
            { revision: 11, at: 60_000, value: "1" },
        ].forEach((observation) => {
            ["autoDirToy", "autoDirBeads"].forEach((varName) => {
                const result = learner.observe({
                    targetIniPath,
                    varName,
                    value: observation.value,
                    revision: observation.revision,
                    at: observation.at,
                });
                result.newlySuppressed.forEach((name) => suppressed.add(name));
                result.newlyLearned.forEach((variable) => learned.add(variable.name));
            });
        });

        assert.deepEqual([...suppressed].sort(), ["Phase", "autoDirBeads", "autoDirToy"].sort());
        assert.deepEqual([...learned].sort(), ["autoDirBeads", "autoDirToy"].sort());
    });

    it("learns correlated phase, output, and sparse direction variables as one runtime cohort", () => {
        const learner = new TogglePersistLearner();
        const suppressed = new Set<string>();
        const learned = new Set<string>();

        Array.from({ length: 21 }, (_, index) => index).forEach((index) => {
            const revision = index + 1;
            const at = index * 3_000;
            const phase = index <= 10 ? index / 10 : (20 - index) / 10;
            [
                ["autoPhaseToy", phase],
                ["Freq_shape_7", phase],
                ["autoPhaseBeads", phase],
                ["Freq_shape_6", phase],
            ].forEach(([varName, value]) => {
                const result = learner.observe({
                    targetIniPath,
                    varName: String(varName),
                    value: String(value),
                    revision,
                    at,
                });
                result.newlySuppressed.forEach((name) => suppressed.add(name));
                result.newlyLearned.forEach((variable) => learned.add(variable.name));
            });

            if (index % 5 === 0) {
                ["autoDirToy", "autoDirBeads"].forEach((varName) => {
                    const result = learner.observe({
                        targetIniPath,
                        varName,
                        value: index % 10 === 0 ? "1" : "-1",
                        revision,
                        at,
                    });
                    result.newlySuppressed.forEach((name) => suppressed.add(name));
                    result.newlyLearned.forEach((variable) => learned.add(variable.name));
                });
            }
        });

        assert.deepEqual(
            [...suppressed].sort(),
            [
                "Freq_shape_6",
                "Freq_shape_7",
                "autoDirBeads",
                "autoDirToy",
                "autoPhaseBeads",
                "autoPhaseToy",
            ].sort(),
        );
        assert.deepEqual([...learned].sort(), [...suppressed].sort());
    });

    it("saves an irregular sparse sequence instead of permanently suppressing it", () => {
        const learner = new TogglePersistLearner();
        [0, 20_000, 55_000, 90_000].forEach((at, index) => {
            const result = learner.observe({
                targetIniPath,
                varName: "Sparse",
                value: String(index),
                revision: [1, 3, 10, 15][index],
                at,
            });
            assert.deepEqual(result.newlySuppressed, []);
        });

        assert.deepEqual([...learner.takeReady(targetIniPath, 100_000).updates], [["sparse", "3"]]);
    });

    it("reconsiders an isolated change after a suppressed stream cools down", () => {
        const learner = new TogglePersistLearner();
        Array.from({ length: 10 }, (_, index) => {
            learner.observe({
                targetIniPath,
                varName: "Phase",
                value: String(index),
                revision: index + 1,
                at: index * 2_000,
            });
        });

        learner.observe({
            targetIniPath,
            varName: "Phase",
            value: "manual",
            revision: 20,
            at: 50_000,
        });
        assert.deepEqual(
            [...learner.takeReady(targetIniPath, 53_000).updates],
            [["phase", "manual"]],
        );
    });

    it("uses a learned profile as a prior without blocking isolated changes", () => {
        const learner = new TogglePersistLearner();
        learner.registerLearnedVariables(targetIniPath, {
            phase: { name: "Phase", medianIntervalMs: 1_000, learnedAt: new Date(0).toISOString() },
        });
        learner.observe({ targetIniPath, varName: "Phase", value: "0.5", revision: 1, at: 0 });
        assert.deepEqual([...learner.takeReady(targetIniPath, 3_000).updates], [["phase", "0.5"]]);

        const repeated = new TogglePersistLearner();
        repeated.registerLearnedVariables(targetIniPath, {
            phase: { name: "Phase", medianIntervalMs: 1_000, learnedAt: new Date(0).toISOString() },
        });
        const suppressed = [0, 1_000, 2_000].flatMap(
            (at, index) =>
                repeated.observe({
                    targetIniPath,
                    varName: "Phase",
                    value: String(index),
                    revision: index + 1,
                    at,
                }).newlySuppressed,
        );
        assert.deepEqual(suppressed, ["Phase"]);
        assert.equal(repeated.takeReady(targetIniPath, 20_000).updates.size, 0);

        const differentCadence = new TogglePersistLearner();
        differentCadence.registerLearnedVariables(targetIniPath, {
            phase: { name: "Phase", medianIntervalMs: 1_000, learnedAt: new Date(0).toISOString() },
        });
        [0, 3_000, 6_000].forEach((at, index) => {
            assert.deepEqual(
                differentCadence.observe({
                    targetIniPath,
                    varName: "Phase",
                    value: String(index),
                    revision: index + 1,
                    at,
                }).newlySuppressed,
                [],
            );
        });
        assert.deepEqual(
            [...differentCadence.takeReady(targetIniPath, 15_000).updates],
            [["phase", "2"]],
        );
    });

    it("normalizes persist values when fingerprinting an INI", () => {
        const first = "[Constants]\r\nglobal persist $Toggle = 0\r\n[Present]\r\npost $Toggle = 1";
        const second = "[Constants]\nglobal persist $Toggle = 9\n[Present]\npost $Toggle = 1";
        const structuralChange =
            "[Constants]\nglobal persist $Toggle = 9\n[Present]\npost $Toggle = 2";

        assert.equal(fingerprintTogglePersistIni(first), fingerprintTogglePersistIni(second));
        assert.notEqual(
            fingerprintTogglePersistIni(second),
            fingerprintTogglePersistIni(structuralChange),
        );
    });

    it("validates and normalizes profile variable keys", () => {
        const profile = parseTogglePersistProfile({
            version: 1,
            files: {
                "example.ini": {
                    fingerprint: "hash",
                    variables: {
                        Phase: {
                            name: "Phase",
                            medianIntervalMs: 1_000,
                            learnedAt: new Date(0).toISOString(),
                        },
                    },
                },
            },
        });

        assert.equal(profile.files["example.ini"].variables.phase.name, "Phase");
        assert.throws(() => parseTogglePersistProfile({ version: 2, files: {} }));
    });
});
