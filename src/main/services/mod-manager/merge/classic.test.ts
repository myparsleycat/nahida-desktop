import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { writeClassicMerge } from "./classic.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

describe("writeClassicMerge", () => {
    it("writes merged.ini and disables source inis", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-classic-"));
        tempRoots.push(root);
        const aDir = path.join(root, "A");
        const bDir = path.join(root, "B");
        await fse.ensureDir(aDir);
        await fse.ensureDir(bDir);
        const aIni = path.join(aDir, "Klee.ini");
        const bIni = path.join(bDir, "Klee.ini");
        await fse.writeFile(
            aIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
[ResourcePosition]
filename = A.buf
`,
        );
        await fse.writeFile(
            bIni,
            `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
[ResourcePosition]
filename = B.buf
`,
        );

        const output = await writeClassicMerge({
            outputDir: root,
            sources: [
                { iniPath: aIni, groupIndex: 0 },
                { iniPath: bIni, groupIndex: 1 },
            ],
            forwardKey: "vk_right",
            backKey: "vk_left",
        });

        const text = await fse.readFile(output, "utf8");
        assert.match(text, /; Merged Mod:/);
        assert.match(text, /\$swapvar = 0,1/);
        assert.match(text, /\[KeySwap\]/);
        assert.match(text, /\[CommandListKleePosition\]/);
        assert.match(text, /\[ResourcePosition\.0\]/);
        assert.equal(await fse.pathExists(aIni), false);
        assert.equal(await fse.pathExists(path.join(aDir, "DISABLEDKlee.ini")), true);
    });
});
