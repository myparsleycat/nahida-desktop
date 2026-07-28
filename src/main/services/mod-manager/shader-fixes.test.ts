import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, beforeEach, describe, it, vi } from "vitest";

import type { NahidaDesktop } from "../..";
import type { ModLibraryService } from "./library";
import type { ShaderFixesProcessedFile } from "./shader-fixes";

import { ModShaderFixesService, SHADER_FIXES_MOD_MARKER_FILE } from "./shader-fixes";

const fastGlobCalls = vi.hoisted(
    () => [] as { pattern: string | string[]; cwd: string | undefined }[],
);
const atomicWriteState = vi.hoisted(() => ({ calls: 0, failOnCall: null as number | null }));

vi.mock("fast-glob", async (importOriginal) => {
    const original = await importOriginal<typeof import("fast-glob")>();
    const fastGlob = Reflect.get(original, "default") as typeof import("fast-glob");
    return {
        ...original,
        default: new Proxy(fastGlob, {
            apply(target, thisArg, args) {
                fastGlobCalls.push({
                    pattern: args[0] as string | string[],
                    cwd: (args[1] as { cwd?: string } | undefined)?.cwd,
                });
                return Reflect.apply(target, thisArg, args);
            },
        }),
    };
});

vi.mock("write-file-atomic", async (importOriginal) => {
    const original = await importOriginal<typeof import("write-file-atomic")>();
    const writeFileAtomic = Reflect.get(original, "default") as typeof import("write-file-atomic");
    return {
        ...original,
        default: new Proxy(writeFileAtomic, {
            apply(target, thisArg, args) {
                atomicWriteState.calls += 1;
                if (atomicWriteState.calls === atomicWriteState.failOnCall) {
                    throw new Error("OWNER_INDEX_WRITE_FAILED");
                }
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
    let importers: { key: string; importerFolder: string }[];
    let games: { game: string; modFolderPath: string; importer: string }[];

    beforeEach(async () => {
        rootPath = await fse.mkdtemp(path.join(os.tmpdir(), "nahida-shader-fixes-"));
        importerPath = path.join(rootPath, "Importer");
        modsPath = path.join(importerPath, "Mods");
        await fse.ensureDir(modsPath);
        importers = [{ key: "Importer", importerFolder: importerPath }];
        games = [{ game: "Test", modFolderPath: modsPath, importer: "Importer" }];

        service = new ModShaderFixesService(
            {
                service: {
                    xxmi: {
                        getEnabledImporters: () => importers,
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
                games: async () => games,
            } as unknown as ModLibraryService,
        );

        fastGlobCalls.length = 0;
        atomicWriteState.calls = 0;
        atomicWriteState.failOnCall = null;
    });

    afterEach(async () => {
        await fse.remove(rootPath);
    });

    it("uses the central owner index without scanning when disabling", async () => {
        const modPath = path.join(modsPath, "Group", "Mod");
        await Promise.all(
            ["a.ini", "nested/b.ini", "nested/c.ini"].map(async (file) => {
                await fse.outputFile(path.join(modPath, "ShaderFixes", file), file);
            }),
        );
        await service.handleShaders(modPath, true);
        assert.equal(atomicWriteState.calls, 2);
        await fse.remove(path.join(modPath, "ShaderFixes"));
        fastGlobCalls.length = 0;

        await service.handleShaders(modPath, false);

        assert.deepEqual(fastGlobCalls, []);
        assert.equal(await fse.pathExists(path.join(importerPath, "ShaderFixes", "a.ini")), false);
        assert.equal(await fse.pathExists(path.join(modPath, SHADER_FIXES_MOD_MARKER_FILE)), false);
        assert.deepEqual(
            await fse.readJson(
                path.join(importerPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE),
            ),
            { version: 1, targets: {} },
        );
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

    it("migrates existing manifests by scanning only the current importer mods", async () => {
        const firstModPath = path.join(modsPath, "Group", "First");
        const secondModPath = path.join(modsPath, "Group", "Second");
        const targetPath = path.join(importerPath, "ShaderFixes", "shared.ini");
        const hash = createHash("sha256").update("shared").digest("hex");
        await fse.outputFile(targetPath, "shared");
        await Promise.all(
            [
                { modPath: firstModPath, modKey: "first" },
                { modPath: secondModPath, modKey: "second" },
            ].map(async ({ modPath, modKey }) => {
                await fse.outputJson(path.join(modPath, SHADER_FIXES_MOD_MARKER_FILE), {
                    version: 1,
                    modKey,
                    files: [
                        {
                            file: "shared.ini",
                            targetPath,
                            targetKey: "legacy-target-key",
                            hash,
                        },
                    ],
                });
            }),
        );
        const otherImporterPath = path.join(rootPath, "OtherImporter");
        const otherModsPath = path.join(otherImporterPath, "Mods");
        await fse.ensureDir(otherModsPath);
        importers.push({ key: "OtherImporter", importerFolder: otherImporterPath });
        games.push({ game: "Other", modFolderPath: otherModsPath, importer: "OtherImporter" });
        fastGlobCalls.length = 0;

        await service.handleShaders(firstModPath, false);

        assert.deepEqual(fastGlobCalls, [
            { pattern: `**/${SHADER_FIXES_MOD_MARKER_FILE}`, cwd: modsPath },
        ]);
        assert.equal(await fse.pathExists(targetPath), true);
        assert.deepEqual(
            await fse.readJson(
                path.join(importerPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE),
            ),
            {
                version: 1,
                targets: { "shared.ini": { hash, owners: ["second"] } },
            },
        );

        fastGlobCalls.length = 0;
        await service.handleShaders(secondModPath, false);
        assert.deepEqual(fastGlobCalls, []);
        assert.equal(await fse.pathExists(targetPath), false);
    });

    it("cleans the original importer after an enabled mod is moved", async () => {
        const originalModPath = path.join(modsPath, "Group", "Mod");
        const originalTargetPath = path.join(importerPath, "ShaderFixes", "moved.ini");
        await fse.outputFile(path.join(originalModPath, "ShaderFixes", "moved.ini"), "moved");
        await service.handleShaders(originalModPath, true);

        const otherImporterPath = path.join(rootPath, "OtherImporter");
        const otherModsPath = path.join(otherImporterPath, "Mods");
        const movedModPath = path.join(otherModsPath, "Group", "Mod");
        await fse.ensureDir(path.dirname(movedModPath));
        importers.push({ key: "OtherImporter", importerFolder: otherImporterPath });
        games.push({ game: "Other", modFolderPath: otherModsPath, importer: "OtherImporter" });
        await fse.move(originalModPath, movedModPath);
        await fse.remove(path.join(importerPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE));
        fastGlobCalls.length = 0;

        await service.handleShaders(movedModPath, false);

        assert.deepEqual(fastGlobCalls, [
            { pattern: `**/${SHADER_FIXES_MOD_MARKER_FILE}`, cwd: modsPath },
        ]);
        assert.equal(await fse.pathExists(originalTargetPath), false);
        assert.equal(
            await fse.pathExists(path.join(movedModPath, SHADER_FIXES_MOD_MARKER_FILE)),
            false,
        );
        assert.deepEqual(
            await fse.readJson(
                path.join(importerPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE),
            ),
            { version: 1, targets: {} },
        );
        assert.equal(
            await fse.pathExists(
                path.join(otherImporterPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE),
            ),
            false,
        );
    });

    it("rebuilds a corrupted owner index before deleting files", async () => {
        const modPath = path.join(modsPath, "Group", "Mod");
        const targetPath = path.join(importerPath, "ShaderFixes", "legacy.ini");
        const hash = createHash("sha256").update("legacy").digest("hex");
        await fse.outputFile(targetPath, "legacy");
        await fse.outputJson(path.join(modPath, SHADER_FIXES_MOD_MARKER_FILE), {
            version: 1,
            modKey: "legacy",
            files: [
                {
                    file: "legacy.ini",
                    targetPath,
                    targetKey: "legacy-target-key",
                    hash,
                },
            ],
        });
        await fse.writeFile(
            path.join(importerPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE),
            "invalid",
        );
        fastGlobCalls.length = 0;

        await service.handleShaders(modPath, false);

        assert.deepEqual(fastGlobCalls, [
            { pattern: `**/${SHADER_FIXES_MOD_MARKER_FILE}`, cwd: modsPath },
        ]);
        assert.equal(await fse.pathExists(targetPath), false);
    });

    it("removes owner state when an enable operation is rolled back", async () => {
        const modPath = path.join(modsPath, "Group", "Mod");
        const targetPath = path.join(importerPath, "ShaderFixes", "rollback.ini");
        await fse.outputFile(path.join(modPath, "ShaderFixes", "rollback.ini"), "rollback");

        const processedFiles = await service.handleShaders(modPath, true);
        await service.rollbackEnabledShaders(modPath, processedFiles);

        assert.equal(await fse.pathExists(targetPath), false);
        assert.deepEqual(
            await fse.readJson(
                path.join(importerPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE),
            ),
            { version: 1, targets: {} },
        );
        assert.equal(await fse.pathExists(path.join(modPath, SHADER_FIXES_MOD_MARKER_FILE)), false);
    });

    it("can roll back when saving owner state fails during enable", async () => {
        const modPath = path.join(modsPath, "Group", "Mod");
        const targetPath = path.join(importerPath, "ShaderFixes", "rollback.ini");
        await fse.outputFile(path.join(modPath, "ShaderFixes", "rollback.ini"), "rollback");
        atomicWriteState.failOnCall = 2;

        const error = await service
            .handleShaders(modPath, true)
            .catch((failure: unknown) => failure);
        assert.equal(error instanceof Error, true);
        const processedFiles = (error as { processedFiles: ShaderFixesProcessedFile[] })
            .processedFiles;
        assert.equal(processedFiles.length, 1);

        atomicWriteState.failOnCall = null;
        await service.rollbackEnabledShaders(modPath, processedFiles);

        assert.equal(await fse.pathExists(targetPath), false);
        assert.deepEqual(
            await fse.readJson(
                path.join(importerPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE),
            ),
            { version: 1, targets: {} },
        );
    });

    it("does not copy the reserved owner index from a mod ShaderFixes folder", async () => {
        const modPath = path.join(modsPath, "Group", "Mod");
        await fse.outputFile(path.join(modPath, "ShaderFixes", "actual.ini"), "actual");
        await fse.outputFile(
            path.join(modPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE),
            "reserved",
        );

        await service.handleShaders(modPath, true);

        assert.deepEqual(
            await fse.readJson(
                path.join(importerPath, "ShaderFixes", SHADER_FIXES_MOD_MARKER_FILE),
            ),
            {
                version: 1,
                targets: {
                    "actual.ini": {
                        hash: createHash("sha256").update("actual").digest("hex"),
                        owners: [
                            (await fse.readJson(path.join(modPath, SHADER_FIXES_MOD_MARKER_FILE)))
                                .modKey,
                        ],
                    },
                },
            },
        );
    });

    it("does not scan the mod when no manifest exists", async () => {
        const modPath = path.join(modsPath, "Group", "Mod");
        await fse.outputFile(path.join(modPath, "ShaderFixes", "unused.ini"), "unused");
        fastGlobCalls.length = 0;

        await service.handleShaders(modPath, false);

        assert.deepEqual(fastGlobCalls, []);
    });
});
