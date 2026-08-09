import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, beforeEach, describe, it, vi } from "vitest";

import type { NahidaDesktop } from "@/main";

import { TogglePersist } from "./toggle-persist";
import {
    fingerprintTogglePersistIni,
    parseTogglePersistProfile,
    togglePersistProfilePath,
} from "./toggle-persist-learning";

const temporaryDirectories: string[] = [];

describe("TogglePersist", () => {
    beforeEach(() => {
        vi.useFakeTimers({ toFake: ["Date", "setTimeout", "clearTimeout"] });
        vi.setSystemTime(0);
    });

    afterEach(async () => {
        vi.useRealTimers();
        await Promise.all(temporaryDirectories.splice(0).map((dirPath) => fse.remove(dirPath)));
    });

    it("batches watcher changes and writes them after the quiet window", async () => {
        const harness = await createHarness({ Toggle: "0", Amount: "0" });
        await harness.persist.startPersistWatcher();

        await harness.trigger({ Toggle: "1", Amount: "0.75" });
        const debug = harness.persist as unknown as {
            cachedD3dxUserIni: Map<string, Record<string, string>>;
            persistLearner: { getNextDueAt: (targetPath: string) => number | undefined };
        };
        assert.equal(
            debug.cachedD3dxUserIni.get("test")?.["$\\Mods\\Example\\mod.ini\\Toggle"],
            "1",
        );
        assert.equal(debug.persistLearner.getNextDueAt(harness.targetIniPath), 3_000);
        await vi.advanceTimersByTimeAsync(2_999);
        assert.match(await fse.readFile(harness.targetIniPath, "utf-8"), /\$Toggle = 0/);

        await vi.advanceTimersByTimeAsync(1);
        await harness.waitForWrites();
        assert.equal(debug.persistLearner.getNextDueAt(harness.targetIniPath), undefined);
        const updated = await fse.readFile(harness.targetIniPath, "utf-8");
        assert.match(
            updated,
            /\$Toggle = 1/,
            JSON.stringify({ info: harness.info.mock.calls, error: harness.error.mock.calls }),
        );
        assert.match(updated, /\$Amount = 0\.75/);
        assert.equal(
            harness.info.mock.calls.filter(([message]) =>
                String(message).startsWith("Updated persist variables"),
            ).length,
            1,
        );

        await harness.persist.stopPersistWatcher();
    });

    it("revalidates a matching learned profile before suppressing writes", async () => {
        const harness = await createHarness({ Phase: "0" });
        const targetContent = await fse.readFile(harness.targetIniPath, "utf-8");
        await fse.outputJson(togglePersistProfilePath(harness.targetIniPath), {
            version: 1,
            files: {
                "mod.ini": {
                    fingerprint: fingerprintTogglePersistIni(targetContent),
                    variables: {
                        phase: {
                            name: "Phase",
                            medianIntervalMs: 2_000,
                            learnedAt: new Date(0).toISOString(),
                        },
                    },
                },
            },
        });
        await harness.persist.startPersistWatcher();

        for (const value of ["0.1", "0.2", "0.3"]) {
            await harness.trigger({ Phase: value });
            await vi.advanceTimersByTimeAsync(2_000);
        }
        await vi.advanceTimersByTimeAsync(10_000);
        await harness.waitForWrites();

        assert.match(await fse.readFile(harness.targetIniPath, "utf-8"), /\$Phase = 0/);
        assert.equal(
            harness.info.mock.calls.filter(([message]) =>
                String(message).startsWith("Suppressed continuously changing persist variables"),
            ).length,
            1,
        );
        await harness.persist.stopPersistWatcher();
    });

    it("ignores a malformed profile and continues normal persistence", async () => {
        const harness = await createHarness({ Toggle: "0" });
        await fse.outputFile(togglePersistProfilePath(harness.targetIniPath), "{broken");
        await harness.persist.startPersistWatcher();

        await harness.trigger({ Toggle: "1" });
        await vi.advanceTimersByTimeAsync(3_000);
        await harness.waitForWrites();

        assert.match(await fse.readFile(harness.targetIniPath, "utf-8"), /\$Toggle = 1/);
        assert.equal(
            harness.error.mock.calls.filter(([message]) => String(message).includes("stage=load"))
                .length,
            1,
        );
        await harness.persist.stopPersistWatcher();
    });

    it("ignores learned variables when the target INI fingerprint changes", async () => {
        const harness = await createHarness({ Toggle: "0" });
        await fse.outputJson(togglePersistProfilePath(harness.targetIniPath), {
            version: 1,
            files: {
                "mod.ini": {
                    fingerprint: "stale-fingerprint",
                    variables: {
                        toggle: {
                            name: "Toggle",
                            medianIntervalMs: 1_000,
                            learnedAt: new Date(0).toISOString(),
                        },
                    },
                },
            },
        });
        await harness.persist.startPersistWatcher();

        await harness.trigger({ Toggle: "1" });
        await vi.advanceTimersByTimeAsync(3_000);
        await harness.waitForWrites();

        assert.match(await fse.readFile(harness.targetIniPath, "utf-8"), /\$Toggle = 1/);
        await harness.persist.stopPersistWatcher();
    });

    it("cancels pending writes when the watcher stops", async () => {
        const harness = await createHarness({ Toggle: "0" });
        await harness.persist.startPersistWatcher();

        await harness.trigger({ Toggle: "1" });
        await harness.persist.stopPersistWatcher();
        await vi.advanceTimersByTimeAsync(10_000);
        await drainAsyncWork();

        assert.match(await fse.readFile(harness.targetIniPath, "utf-8"), /\$Toggle = 0/);
    });

    it("keeps explicit Save-to-INI calls outside automatic learning", async () => {
        const harness = await createHarness({ Toggle: "0" });
        const result = await harness.persist.persistStateToIni(harness.targetIniPath, {
            Toggle: 1,
        });

        assert.deepEqual(result.updatedVariables, ["Toggle"]);
        assert.match(await fse.readFile(harness.targetIniPath, "utf-8"), /\$Toggle = 1/);
        assert.equal(await fse.pathExists(togglePersistProfilePath(harness.targetIniPath)), false);
    });

    it("learns across periodic flushes and saves a reusable mod profile", async () => {
        const harness = await createHarness({ Phase: "0" });
        await harness.persist.startPersistWatcher();

        for (let index = 1; index <= 12; index++) {
            await harness.trigger({ Phase: String(index / 12) });
            await vi.advanceTimersByTimeAsync(10_000);
            await harness.waitForWrites();
            await vi.advanceTimersByTimeAsync(5_000);
        }

        const profile = parseTogglePersistProfile(
            JSON.parse(
                await fse.readFile(togglePersistProfilePath(harness.targetIniPath), "utf-8"),
            ) as unknown,
        );
        assert.equal(profile.files["mod.ini"].variables.phase.name, "Phase");
        assert.match(
            await fse.readFile(harness.targetIniPath, "utf-8"),
            /\$Phase = 0\.5833333333333334/,
        );
        assert.equal(
            harness.info.mock.calls.filter(([message]) =>
                String(message).startsWith("Updated persist variable"),
            ).length,
            7,
        );
        assert.equal(
            harness.info.mock.calls.filter(([message]) =>
                String(message).startsWith("Suppressed continuously changing persist variables"),
            ).length,
            1,
        );

        await harness.persist.stopPersistWatcher();
    });
});

async function createHarness(initialState: Record<string, string>) {
    const importerFolder = await fse.mkdtemp(path.join(os.tmpdir(), "toggle-persist-test-"));
    temporaryDirectories.push(importerFolder);
    const targetIniPath = path.join(importerFolder, "Mods", "Example", "mod.ini");
    const d3dxPath = path.join(importerFolder, "d3dx_user.ini");
    await fse.outputFile(targetIniPath, renderTargetIni(initialState));
    await fse.outputFile(d3dxPath, renderD3dxUserIni(initialState));

    let watcherCallback:
        | ((eventName: "modify" | "create" | "remove", changedPath: string) => void)
        | undefined;
    const info = vi.fn();
    const error = vi.fn();
    const desktop = {
        service: {
            xxmi: {
                getXXMIPath: vi.fn(async () => importerFolder),
                getXXMIConfig: vi.fn(() => ({})),
                getEnabledImporters: vi.fn(() => [{ key: "test", importerFolder }]),
            },
        },
        setting: { xxmi: { getPersistToggles: vi.fn(async () => true) } },
        lib: {
            watcher: {
                create: vi.fn(async (_path, _options, callback) => {
                    watcherCallback = callback;
                    return "watcher";
                }),
                remove: vi.fn(async () => {}),
            },
            fs: { isPathReadable: vi.fn(async () => true) },
        },
        logger: { info, error },
        window: { main: { window: null } },
        ipc: { postMessageToWindow: vi.fn() },
    } as unknown as NahidaDesktop;
    const persist = new TogglePersist(desktop);

    return {
        persist,
        targetIniPath,
        info,
        error,
        trigger: async (state: Record<string, string>) => {
            await fse.writeFile(d3dxPath, renderD3dxUserIni(state), "utf-8");
            assert.ok(watcherCallback);
            watcherCallback("modify", d3dxPath);
            const queued = persist as unknown as {
                d3dxUserIniChangeLocks: Map<string, Promise<void>>;
            };
            await Promise.all([...queued.d3dxUserIniChangeLocks.values()]);
        },
        waitForWrites: async () => {
            await drainAsyncWork();
            const pending = persist as unknown as {
                persistFileUpdateLocks: Map<string, Promise<unknown>>;
            };
            await Promise.all([...pending.persistFileUpdateLocks.values()]);
        },
    };
}

function renderTargetIni(state: Record<string, string>) {
    return `[Constants]\n${Object.entries(state)
        .map(([name, value]) => `global persist $${name} = ${value}`)
        .join("\n")}\n`;
}

function renderD3dxUserIni(state: Record<string, string>) {
    return `[Constants]\n${Object.entries(state)
        .map(([name, value]) => `$\\Mods\\Example\\mod.ini\\${name} = ${value}`)
        .join("\n")}\n`;
}

async function drainAsyncWork() {
    for (let index = 0; index < 50; index++) {
        await new Promise<void>((resolve) => setImmediate(resolve));
    }
}
