import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import {
    disableIniFile,
    ensureMergeBackup,
    rollbackCreated,
    uniqueMergeDisabledName,
    type RollbackAction,
} from "./rollback.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

describe("uniqueMergeDisabledName", () => {
    it("prefers a merge-specific backup name and numbers collisions", () => {
        const used = new Set(["disabled_backup_klee.ini"]);
        assert.equal(uniqueMergeDisabledName("Klee.ini", used), "DISABLED_BACKUP_2_Klee.ini");
        assert.equal(uniqueMergeDisabledName("Klee.ini", used), "DISABLED_BACKUP_3_Klee.ini");
        assert.equal(uniqueMergeDisabledName("Pack", used), "DISABLED Pack");
        used.add("disabled pack");
        assert.equal(uniqueMergeDisabledName("Pack", used), "DISABLED Pack (2)");
    });
});

describe("disableIniFile", () => {
    it("moves to a free merge backup without overwriting a user DISABLED file", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-rollback-"));
        tempRoots.push(root);
        const active = path.join(root, "MasterBeta.ini");
        const disabled = path.join(root, "DISABLEDMasterBeta.ini");
        const backup = path.join(root, "DISABLED_BACKUP_MasterBeta.ini");
        await fse.writeFile(active, "active-master");
        await fse.writeFile(disabled, "stale-backup");

        const created: RollbackAction[] = [];
        await disableIniFile(active, created);
        assert.equal(await fse.pathExists(active), false);
        assert.equal(await fse.readFile(disabled, "utf8"), "stale-backup");
        assert.equal(await fse.readFile(backup, "utf8"), "active-master");

        await rollbackCreated(created);
        assert.equal(await fse.readFile(active, "utf8"), "active-master");
        assert.equal(await fse.readFile(disabled, "utf8"), "stale-backup");
        assert.equal(await fse.pathExists(backup), false);
    });
});

describe("ensureMergeBackup", () => {
    it("ignores an unrelated DISABLED file and records a merge-specific copy", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-rollback-"));
        tempRoots.push(root);
        const active = path.join(root, "Klee.ini");
        const disabled = path.join(root, "DISABLEDKlee.ini");
        const backup = path.join(root, "DISABLED_BACKUP_Klee.ini");
        await fse.writeFile(active, "original");
        await fse.writeFile(disabled, "user-disabled");

        const created: RollbackAction[] = [];
        await ensureMergeBackup(active, created);
        assert.equal(await fse.readFile(disabled, "utf8"), "user-disabled");
        assert.equal(await fse.readFile(backup, "utf8"), "original");

        await ensureMergeBackup(active, created);
        assert.equal((await fse.readdir(root)).filter((name) => /backup/i.test(name)).length, 1);

        await rollbackCreated(created);
        assert.equal(await fse.pathExists(backup), false);
        assert.equal(await fse.readFile(disabled, "utf8"), "user-disabled");
    });
});

describe("rollbackCreated", () => {
    it("returns an empty failure list when all actions succeed", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-rollback-"));
        tempRoots.push(root);
        const file1 = path.join(root, "file1.txt");
        const file2 = path.join(root, "file2.txt");
        await fse.writeFile(file1, "original-1");
        await fse.writeFile(file2, "created-2");

        const failures = await rollbackCreated([
            { kind: "restore", path: file1, contents: "restored-1" },
            { kind: "remove", path: file2 },
        ]);

        assert.deepEqual(failures, []);
        assert.equal(await fse.readFile(file1, "utf8"), "restored-1");
        assert.equal(await fse.pathExists(file2), false);
    });

    it("isolates action errors, collects failures, and continues remaining actions", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-rollback-"));
        tempRoots.push(root);
        const file1 = path.join(root, "file1.txt");
        const file2 = path.join(root, "file2.txt");
        await fse.writeFile(file1, "file1-data");
        await fse.writeFile(file2, "file2-data");

        const invalidDir = path.join(root, "not-a-file");
        await fse.mkdir(invalidDir);

        const failures = await rollbackCreated([
            { kind: "remove", path: file1 },
            { kind: "restore", path: invalidDir, contents: "will-fail-directory-write" },
            { kind: "remove", path: file2 },
        ]);

        assert.equal(await fse.pathExists(file2), false);
        assert.equal(failures.length, 1);
        assert.equal(failures[0].action.kind, "restore");
        assert.equal(await fse.pathExists(file1), false);
    });
});
