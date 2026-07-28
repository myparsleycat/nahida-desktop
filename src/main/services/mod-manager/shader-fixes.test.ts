import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, beforeEach, describe, it, vi } from "vitest";

import type { NahidaDesktop } from "../..";
import type { ModLibraryService } from "./library";

import { ModShaderFixesService, SHADER_FIXES_MOD_MARKER_FILE } from "./shader-fixes";

const fastGlobCalls = vi.hoisted(() => [] as (string | string[])[]);

vi.mock("fast-glob", async (importOriginal) => {
    const original = await importOriginal<typeof import("fast-glob")>();
    const fastGlob = Reflect.get(original, "default") as typeof import("fast-glob");
    return {
        ...original,
        default: new Proxy(fastGlob, {
            apply(target, thisArg, args) {
                fastGlobCalls.push(args[0] as string | string[]);
                return Reflect.apply(target, thisArg, args);
            },
        }),
    };
});

describe("ModShaderFixesService", () => {
    let rootPath: string;
    let importerPath: string;
    let modsPath: string;
    let service: ModShaderFixesService;

    beforeEach(async () => {
        rootPath = await fse.mkdtemp(path.join(os.tmpdir(), "nahida-shader-fixes-"));
        importerPath = path.join(rootPath, "Importer");
        modsPath = path.join(importerPath, "Mods");
        await fse.ensureDir(modsPath);

        service = new ModShaderFixesService(
            {
                service: {
                    xxmi: {
                        getEnabledImporters: () => [
                            { key: "Importer", importerFolder: importerPath },
                        ],
                    },
                },
                lib: {
                    utils: {
                        getFileHash: async (filePath: string) =>
                            createHash("sha256")
                                .update(await fse.readFile(filePath))
                                .digest("hex"),
                    },
                },
                logger: { error: vi.fn() },
            } as unknown as NahidaDesktop,
            {
                games: async () => [{ modFolderPath: modsPath }],
            } as unknown as ModLibraryService,
        );

        fastGlobCalls.length = 0;
    });

    afterEach(async () => {
        await fse.remove(rootPath);
    });

    it("uses the manifest directly and scans owners once when disabling", async () => {
        const modPath = path.join(modsPath, "Group", "Mod");
        await Promise.all(
            ["a.ini", "nested/b.ini", "nested/c.ini"].map(async (file) => {
                await fse.outputFile(path.join(modPath, "ShaderFixes", file), file);
            }),
        );
        await service.handleShaders(modPath, true);
        await fse.remove(path.join(modPath, "ShaderFixes"));
        fastGlobCalls.length = 0;

        await service.handleShaders(modPath, false);

        assert.deepEqual(fastGlobCalls, [`**/${SHADER_FIXES_MOD_MARKER_FILE}`]);
        assert.equal(await fse.pathExists(path.join(importerPath, "ShaderFixes", "a.ini")), false);
        assert.equal(await fse.pathExists(path.join(modPath, SHADER_FIXES_MOD_MARKER_FILE)), false);
    });

    it("preserves a shader fix while another mod owns it", async () => {
        const firstModPath = path.join(modsPath, "Group", "First");
        const secondModPath = path.join(modsPath, "Group", "Second");
        await Promise.all(
            [firstModPath, secondModPath].map(async (modPath) => {
                await fse.outputFile(path.join(modPath, "ShaderFixes", "shared.ini"), "shared");
                await service.handleShaders(modPath, true);
            }),
        );

        await service.handleShaders(firstModPath, false);
        assert.equal(
            await fse.pathExists(path.join(importerPath, "ShaderFixes", "shared.ini")),
            true,
        );

        await service.handleShaders(secondModPath, false);
        assert.equal(
            await fse.pathExists(path.join(importerPath, "ShaderFixes", "shared.ini")),
            false,
        );
    });

    it("preserves a shader fix that changed after it was copied", async () => {
        const modPath = path.join(modsPath, "Group", "Mod");
        const targetPath = path.join(importerPath, "ShaderFixes", "changed.ini");
        await fse.outputFile(path.join(modPath, "ShaderFixes", "changed.ini"), "original");
        await service.handleShaders(modPath, true);
        await fse.writeFile(targetPath, "changed");

        await service.handleShaders(modPath, false);

        assert.equal(await fse.readFile(targetPath, "utf8"), "changed");
        assert.equal(await fse.pathExists(path.join(modPath, SHADER_FIXES_MOD_MARKER_FILE)), false);
    });

    it("does not scan the mod when no manifest exists", async () => {
        const modPath = path.join(modsPath, "Group", "Mod");
        await fse.outputFile(path.join(modPath, "ShaderFixes", "unused.ini"), "unused");
        fastGlobCalls.length = 0;

        await service.handleShaders(modPath, false);

        assert.deepEqual(fastGlobCalls, []);
    });
});
