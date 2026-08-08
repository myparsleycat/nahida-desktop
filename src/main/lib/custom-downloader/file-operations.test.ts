import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, expect, it, vi } from "vitest";

import { finalizeStagedDownload } from "./file-operations";

describe("finalizeStagedDownload", () => {
    const tempRoots: string[] = [];

    afterEach(async () => {
        vi.restoreAllMocks();
        await Promise.all(tempRoots.splice(0).map((dir) => fse.remove(dir).catch(() => {})));
    });

    async function createTempRoot() {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nahida-finalize-"));
        tempRoots.push(root);
        return root;
    }

    it("preserves successfully restored entries when a later restore fails and is retried", async () => {
        const root = await createTempRoot();
        const stagingPath = path.join(root, "staging");
        const destinationDir = path.join(root, "dest");
        await fse.ensureDir(stagingPath);
        await fse.ensureDir(destinationDir);

        await fse.writeFile(path.join(destinationDir, "a.txt"), "original-a");
        await fse.writeFile(path.join(destinationDir, "b.txt"), "original-b");
        await fse.writeFile(path.join(stagingPath, "a.txt"), "new-a");
        await fse.writeFile(path.join(stagingPath, "b.txt"), "new-b");

        const finalized = await finalizeStagedDownload(stagingPath, destinationDir);
        expect(await fse.readFile(path.join(destinationDir, "a.txt"), "utf8")).toBe("new-a");
        expect(await fse.readFile(path.join(destinationDir, "b.txt"), "utf8")).toBe("new-b");

        const realMove = fse.move.bind(fse);
        let backupRestores = 0;
        vi.spyOn(fse, "move").mockImplementation(async (src, dest, options) => {
            if (
                typeof src === "string" &&
                src.includes(".nhd-backup-") &&
                path.basename(dest as string) === "b.txt"
            ) {
                backupRestores += 1;
                if (backupRestores === 1) {
                    throw new Error("simulated restore failure for b.txt");
                }
            }
            return realMove(src, dest, options);
        });

        await expect(finalized.restore()).rejects.toThrow("simulated restore failure for b.txt");

        expect(await fse.readFile(path.join(destinationDir, "a.txt"), "utf8")).toBe("original-a");
        expect(await fse.pathExists(path.join(destinationDir, "b.txt"))).toBe(false);

        await finalized.restore();

        expect(await fse.readFile(path.join(destinationDir, "a.txt"), "utf8")).toBe("original-a");
        expect(await fse.readFile(path.join(destinationDir, "b.txt"), "utf8")).toBe("original-b");
    });

    it("no-ops restore after commit", async () => {
        const root = await createTempRoot();
        const stagingPath = path.join(root, "staging");
        const destinationDir = path.join(root, "dest");
        await fse.ensureDir(stagingPath);
        await fse.ensureDir(destinationDir);

        await fse.writeFile(path.join(destinationDir, "a.txt"), "original-a");
        await fse.writeFile(path.join(stagingPath, "a.txt"), "new-a");

        const finalized = await finalizeStagedDownload(stagingPath, destinationDir);
        await finalized.commit();
        await finalized.restore();

        expect(await fse.readFile(path.join(destinationDir, "a.txt"), "utf8")).toBe("new-a");
    });
});
