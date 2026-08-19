import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { classifyMergePacks } from "./classify.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

async function makePack(name: string, files: Record<string, string>) {
    const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-merge-"));
    tempRoots.push(root);
    const pack = path.join(root, name);
    await Promise.all(
        Object.entries(files).map(async ([relative, text]) => {
            const target = path.join(pack, relative);
            await fse.ensureDir(path.dirname(target));
            await fse.writeFile(target, text);
        }),
    );
    return pack;
}

const ordinaryIni = `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition

[ResourcePosition]
filename = KleePosition.buf
`;

describe("classifyMergePacks", () => {
    it("classifies ordinary, toggle, classic, namespace, and support packs", async () => {
        const ordinary = await makePack("Ordinary", { "Klee.ini": ordinaryIni });
        const toggle = await makePack("Toggle", {
            "Klee.ini": `${ordinaryIni}
[Constants]
global persist $dress = 0

[KeyDress]
type = cycle
$dress = 0,1
`,
        });
        const classic = await makePack("Classic", {
            "merged.ini": `; Merged Mod: a.ini, b.ini
[Constants]
global persist $swapvar = 0
[CommandListKleePosition]
if $swapvar == 0
	vb0 = ResourcePosition.0
endif
[ResourcePosition.0]
filename = a.buf
`,
        });
        const namespaced = await makePack("Namespaced", {
            "MasterKlee.ini": `; Merged Mod: child.ini
namespace = Klee\\Master
[Constants]
global persist $swapvar = 0
[TextureOverrideKleePosition]
hash = abcdef01
$active = 1
`,
            "child.ini": `[TextureOverrideKleePosition]
hash = abcdef01
match_priority = 0
if $\\Klee\\Master\\swapvar==0
	vb0 = ResourcePosition
endif
`,
        });
        const support = await makePack("Support", {
            "ORFix.ini": "run = CommandList\\global\\ORFix\n",
        });
        const folderDisabled = await makePack("DISABLED Ordinary", { "Klee.ini": ordinaryIni });
        const fileDisabled = await makePack("FileDisabled", {
            "DISABLEDKlee.ini": ordinaryIni,
            "help.ini": "[KeyHelp]\n",
        });

        const result = await classifyMergePacks([
            ordinary,
            toggle,
            classic,
            namespaced,
            support,
            folderDisabled,
            fileDisabled,
        ]);

        assert.equal(result.packs.find((pack) => pack.path === ordinary)?.family, "ordinary");
        assert.equal(result.packs.find((pack) => pack.path === toggle)?.family, "in_mod_toggle");
        assert.equal(result.packs.find((pack) => pack.path === classic)?.family, "classic_merge");
        assert.equal(
            result.packs.find((pack) => pack.path === namespaced)?.family,
            "namespace_merge",
        );
        assert.equal(result.packs.find((pack) => pack.path === support)?.family, "support");
        assert.equal(result.packs.find((pack) => pack.path === folderDisabled)?.family, "ordinary");
        assert.equal(result.packs.find((pack) => pack.path === fileDisabled)?.family, "support");
        assert.equal(result.packs.find((pack) => pack.path === ordinary)?.allowsClassic, true);
        assert.equal(result.packs.find((pack) => pack.path === toggle)?.allowsClassic, false);
    });

    it("locks classic when an ordinary pack contains control-flow lines", async () => {
        const conditional = await makePack("Conditional", {
            "Klee.ini": `[TextureOverrideKleePosition]
hash = abcdef01
if DRAW_TYPE == 1
	vb0 = ResourcePosition
endif

[ResourcePosition]
filename = KleePosition.buf
`,
        });
        const result = await classifyMergePacks([conditional]);
        assert.equal(result.packs[0].family, "ordinary");
        assert.equal(result.packs[0].allowsClassic, false);
    });

    it("locks classic for WWMI dumps even when they look ordinary", async () => {
        const wwmi = await makePack("Camellya", {
            "mod.ini": `; WWMI ALPHA-2 INI
[Constants]
global $object_guid = 170961
[TextureOverrideComponent0]
hash = 7748c1d8
`,
        });
        const result = await classifyMergePacks([wwmi]);
        assert.equal(result.packs[0].dialect, "wwmi");
        assert.equal(result.packs[0].allowsClassic, false);
    });

    it("warns when hashes do not overlap but still returns packs", async () => {
        const left = await makePack("Left", { "A.ini": ordinaryIni });
        const right = await makePack("Right", {
            "B.ini": ordinaryIni.replace("abcdef01", "12345678"),
        });
        const result = await classifyMergePacks([left, right]);
        assert.equal(result.hashOverlap, false);
        assert.equal(result.warnings.includes("hash_mismatch"), true);
    });
});
