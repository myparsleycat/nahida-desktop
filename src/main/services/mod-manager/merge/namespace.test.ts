import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { unwrapNamespace, wrapHashedSections, writeNamespaceMerge } from "./namespace.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

const childIni = `[TextureOverrideKleePosition]
hash = abcdef01
match_priority = 0
vb0 = ResourcePosition
`;

describe("namespace merge writer", () => {
    it("wraps hashed sections and writes a master stub", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-ns-"));
        tempRoots.push(root);
        const childPath = path.join(root, "Klee.ini");
        await fse.writeFile(childPath, childIni);

        const masterPath = await writeNamespaceMerge({
            masterDir: root,
            name: "Klee",
            sources: [{ iniPath: childPath, index: 0 }],
            forwardKey: "]",
            backKey: "[",
            includeVanilla: false,
        });

        const master = await fse.readFile(masterPath, "utf8");
        const child = await fse.readFile(childPath, "utf8");
        assert.match(master, /namespace = Klee\\Master/);
        assert.match(master, /hash = abcdef01/);
        assert.match(child, /if \$\\Klee\\Master\\swapvar==0/);
        assert.match(child, /endif/);
        assert.equal(await fse.pathExists(path.join(root, "DISABLEDKlee.ini")), true);
    });

    it("unwraps an existing namespace wrap before remastering", async () => {
        const wrapped = wrapHashedSections(childIni, "Old", 3);
        const unwrapped = await unwrapNamespace(wrapped);
        assert.doesNotMatch(unwrapped, /\$\\Old\\Master\\swapvar/);
        assert.match(unwrapped, /hash = abcdef01/);
        assert.match(unwrapped, /match_priority = 0/);
    });
});
