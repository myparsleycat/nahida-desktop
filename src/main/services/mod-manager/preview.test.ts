import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, it } from "vitest";

import { findPreview } from "./preview.ts";

const tempRoots: string[] = [];

afterEach(async () => {
    await Promise.all(tempRoots.splice(0).map((root) => fse.remove(root)));
});

async function makeRoot() {
    const root = await fse.mkdtemp(path.join(os.tmpdir(), "nhd-preview-"));
    tempRoots.push(root);
    return root;
}

async function writeFile(root: string, relativePath: string) {
    const filePath = path.join(root, relativePath);
    await fse.ensureDir(path.dirname(filePath));
    await fse.writeFile(filePath, "preview");
    return filePath;
}

describe("findPreview", () => {
    it("accepts a media file that is not named preview", async () => {
        const root = await makeRoot();
        const screenshot = await writeFile(root, "screenshot.png");

        assert.equal(await findPreview(root, false), screenshot);
    });

    it("prefers a preview-named file over a generic root image", async () => {
        const root = await makeRoot();
        const preview = await writeFile(root, "preview.webp");
        await writeFile(root, "cover.jpg");

        assert.equal(await findPreview(root, false), preview);
    });

    it("accepts a file whose name contains preview", async () => {
        const root = await makeRoot();
        const preview = await writeFile(root, "mod_preview.png");

        assert.equal(await findPreview(root, false), preview);
    });

    it("searches nested folders only when asked", async () => {
        const root = await makeRoot();
        const nested = await writeFile(root, "images/cover.jpg");

        assert.equal(await findPreview(root, true), nested);
        assert.equal(await findPreview(root, false), undefined);
    });

    it("prefers a root image over a nested preview in a disabled folder", async () => {
        const root = await makeRoot();
        const screenshot = await writeFile(root, "screenshot.png");
        await writeFile(root, "DISABLED Nested/preview.png");

        assert.equal(await findPreview(root, true), screenshot);
    });

    it("prefers a group-root preview over a child folder", async () => {
        const root = await makeRoot();
        const rootPreview = await writeFile(root, "preview.png");
        await writeFile(root, "Enabled Mod/preview.png");

        assert.equal(await findPreview(root, true), rootPreview);
    });

    it("prefers an enabled child folder before a disabled one", async () => {
        const root = await makeRoot();
        const enabledPreview = await writeFile(root, "Enabled Mod/nested/deeper/preview.png");
        await writeFile(root, "DISABLED Other Mod/preview.png");

        assert.equal(await findPreview(root, true), enabledPreview);
    });

    it("falls back to a disabled child folder", async () => {
        const root = await makeRoot();
        const disabledPreview = await writeFile(root, "DISABLED Other Mod/preview.png");

        assert.equal(await findPreview(root, true), disabledPreview);
    });

    it("accepts a non-preview filename in a child folder", async () => {
        const root = await makeRoot();
        const cover = await writeFile(root, "Enabled Mod/cover.jpg");

        assert.equal(await findPreview(root, true), cover);
    });

    it("recognizes a disabled preview file suffix", async () => {
        const root = await makeRoot();
        const preview = await writeFile(root, "preview.png.disabled");

        assert.equal(await findPreview(root, false), preview);
    });

    it("ignores texture-like names", async () => {
        const root = await makeRoot();
        await writeFile(root, "normal.png");
        await writeFile(root, "light.jpg");
        await writeFile(root, "material.webp");
        await writeFile(root, "diffuse.png");

        assert.equal(await findPreview(root, false), undefined);
    });
});
