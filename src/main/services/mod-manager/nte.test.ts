import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import type { NahidaDesktop } from "../..";

import { getNteListingGroupPath, getNteMods, getNteSubGroups, listNteModPaths } from "./nte.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

async function makeModRoot() {
    const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-nte-"));
    tempRoots.push(root);
    return root;
}

async function writePak(dirPath: string, fileName: string) {
    await fse.ensureDir(dirPath);
    await fse.writeFile(path.join(dirPath, fileName), "pak");
}

function desktopStub() {
    return {
        lib: {
            fs: {
                getFolderSize: async () => 1,
            },
        },
    } as unknown as NahidaDesktop;
}

describe("NTE wrapped pak folders", () => {
    it("flattens a wrapper with inner pak folders into the parent mod list", async () => {
        const modRoot = await makeModRoot();
        const shinku = path.join(modRoot, "Character", "Shinku");
        const sparkle = path.join(shinku, "Shinku Natsu Sparkle 1.3");
        const disabledShinku = path.join(shinku, "DISABLED Shinku");
        const inner = path.join(sparkle, "shinku");
        const burst = path.join(sparkle, "shinku burst");

        await writePak(disabledShinku, "mod_P.pak.disabled");
        await writePak(inner, "Shinku Natsu Sparkle_P.pak");
        await writePak(burst, "Shinku Natsu Sparkle burst_P.pak");
        await fse.writeFile(path.join(sparkle, "preview.png"), "preview");

        const roots = { modRoot, linkedRoot: null };
        const group = await getNteMods(desktopStub(), roots, shinku);

        assert.deepEqual(
            group.mods.map((mod) => ({ name: mod.name, isEnabled: mod.isEnabled })),
            [
                { name: "DISABLED Shinku", isEnabled: false },
                { name: "Shinku Natsu Sparkle 1.3 / shinku", isEnabled: true },
                { name: "Shinku Natsu Sparkle 1.3 / shinku burst", isEnabled: true },
            ],
        );
        assert.equal(group.modCount, 3);
        assert.equal(group.enabledModCount, 2);
        assert.equal(
            group.mods.find((mod) => mod.name === "Shinku Natsu Sparkle 1.3 / shinku")?.preview,
            path.join(sparkle, "preview.png"),
        );
        assert.deepEqual(await listNteModPaths(shinku), [disabledShinku, inner, burst]);
        assert.equal(await getNteListingGroupPath(inner), shinku);
        assert.equal(await getNteListingGroupPath(disabledShinku), shinku);
    });

    it("does not list a pak wrapper as a subgroup", async () => {
        const modRoot = await makeModRoot();
        const shinku = path.join(modRoot, "Character", "Shinku");
        await writePak(path.join(shinku, "DISABLED Shinku"), "mod_P.pak.disabled");
        await writePak(
            path.join(shinku, "Shinku Natsu Sparkle 1.3", "shinku"),
            "Shinku Natsu Sparkle_P.pak",
        );

        const groups = await getNteSubGroups(desktopStub(), { modRoot, linkedRoot: null }, shinku);

        assert.deepEqual(
            groups.map((group) => group.name),
            [],
        );
    });

    it("keeps a character folder as a group when it contains both mods and wrappers", async () => {
        const modRoot = await makeModRoot();
        const character = path.join(modRoot, "Character");
        const shinku = path.join(character, "Shinku");
        await writePak(path.join(shinku, "DISABLED Shinku"), "mod_P.pak.disabled");
        await writePak(
            path.join(shinku, "Shinku Natsu Sparkle 1.3", "shinku"),
            "Shinku Natsu Sparkle_P.pak",
        );

        const roots = { modRoot, linkedRoot: null };
        const characterMods = await getNteMods(desktopStub(), roots, character);
        const characterGroups = await getNteSubGroups(desktopStub(), roots, character);
        const shinkuGroup = characterGroups.find((group) => group.name === "Shinku");

        assert.deepEqual(
            characterMods.mods.map((mod) => mod.name),
            [],
        );
        assert.equal(shinkuGroup?.modCount, 2);
        assert.equal(shinkuGroup?.enabledModCount, 1);
        assert.equal(shinkuGroup?.hasSubGroups, false);
    });

    it("still lists NPC mods that have a pak in the folder itself", async () => {
        const modRoot = await makeModRoot();
        const npc = path.join(modRoot, "NPC");
        const shopGirl = path.join(npc, "NPC_Shop Girl");
        await writePak(shopGirl, "NPC_023_NSFW_P.pak");

        const roots = { modRoot, linkedRoot: null };
        const group = await getNteMods(desktopStub(), roots, npc);
        const npcGroups = await getNteSubGroups(desktopStub(), roots, npc);

        assert.deepEqual(
            group.mods.map((mod) => ({ name: mod.name, isEnabled: mod.isEnabled })),
            [{ name: "NPC_Shop Girl", isEnabled: true }],
        );
        assert.deepEqual(
            npcGroups.map((entry) => entry.name),
            [],
        );
    });

    it("flattens variant wrappers like longhairdaff without collapsing them into one toggle", async () => {
        const modRoot = await makeModRoot();
        const daffodil = path.join(modRoot, "Character", "Daffodil");
        await writePak(path.join(daffodil, "longhairdaff", "Alt Skin"), "mod_P.pak");
        await writePak(path.join(daffodil, "longhairdaff", "Default Skin"), "mod_P.pak");

        const group = await getNteMods(desktopStub(), { modRoot, linkedRoot: null }, daffodil);

        assert.deepEqual(
            group.mods.map((mod) => mod.name),
            ["longhairdaff / Alt Skin", "longhairdaff / Default Skin"],
        );
        assert.equal(
            group.mods.every((mod) => mod.isEnabled),
            true,
        );
    });
});
