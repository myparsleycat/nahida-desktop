import path from "node:path";

import { isSafeMergeName, stripDisabledPrefix } from "@shared/mod";
import type {
    MergeModsRequest,
    MergeModsResult,
    MergePackClassification,
    MergePlanGroup,
    MergePlanNode,
} from "@shared/types";
import fse from "fs-extra";

import type { NahidaDesktop } from "../../..";

import { writeClassicMerge } from "./classic";
import { classifyMergePacks, classifyPack, collectEnabledInis } from "./classify";
import { collectNamespaceChildren, writeNamespaceMerge } from "./namespace";
import {
    allocateMergeDisabledPath,
    describeRollbackAction,
    disableIniFile,
    rollbackCreated,
    type RollbackAction,
} from "./rollback";

export class ModMergeService {
    constructor(private readonly desktop: NahidaDesktop) {}

    public classifyMergePacks(modPaths: string[]) {
        return classifyMergePacks(modPaths);
    }

    public async mergeMods(request: MergeModsRequest): Promise<MergeModsResult> {
        const created: RollbackAction[] = [];
        try {
            const outputPath = await this.executeNode(request.root, request, created);
            return { outputPath };
        } catch (error) {
            await rollbackCreated(created);
            this.desktop.logger.error(
                {
                    operation: "mod:mergeMods",
                    groupPath: request.groupPath,
                    placement: request.placement,
                    packName: request.packName,
                    stage: "execute",
                    created: created.map(describeRollbackAction),
                    error: error instanceof Error ? error.message : String(error),
                },
                "Mod:mergeMods:context",
            );
            throw error;
        }
    }

    private async executeNode(
        node: MergePlanNode,
        request: MergeModsRequest,
        created: RollbackAction[],
    ): Promise<string> {
        if (node.kind === "leaf") return node.path;
        const childPaths: string[] = [];
        for (const child of node.children) {
            childPaths.push(await this.executeNode(child, request, created));
        }
        return await this.executeGroup(node, childPaths, request, created);
    }

    private async executeGroup(
        node: MergePlanGroup,
        childPaths: string[],
        request: MergeModsRequest,
        created: RollbackAction[],
    ) {
        const packs = await Promise.all(childPaths.map((childPath) => classifyPack(childPath)));
        assertGroupAllowed(node, packs);

        if (node.engine === "classic") {
            return await this.executeClassic(node, packs, request, created);
        }
        return await this.executeNamespace(node, packs, request, created);
    }

    private async executeClassic(
        node: MergePlanGroup,
        packs: MergePackClassification[],
        request: MergeModsRequest,
        created: RollbackAction[],
    ) {
        const destPaths = await this.prepareClassicWorkDir(node, packs, request, created);
        const sources = (
            await Promise.all(
                packs.map(async (pack, index) => {
                    const mappedPath = destPaths[index];
                    const iniPath = await resolvePrimaryIni(
                        mappedPath,
                        pack.primaryIniPath,
                        pack.path,
                    );
                    return iniPath ? { iniPath, groupIndex: index } : null;
                }),
            )
        ).filter((source): source is { iniPath: string; groupIndex: number } => source !== null);

        const workDir = destPaths[0];
        if (sources.length < 2 || !workDir) {
            throw new Error("CLASSIC_MERGE_NEEDS_TWO_INIS");
        }

        return path.dirname(
            await writeClassicMerge({
                outputDir: path.dirname(workDir),
                sources,
                forwardKey: node.forwardKey,
                backKey: node.backKey || undefined,
                created,
            }),
        );
    }

    private async executeNamespace(
        node: MergePlanGroup,
        packs: MergePackClassification[],
        request: MergeModsRequest,
        created: RollbackAction[],
    ) {
        const workingPacks =
            request.placement === "in_place" ? await enablePackFolders(packs, created) : packs;
        const namespacePacks = workingPacks.filter((pack) => pack.family === "namespace_merge");
        const host =
            namespacePacks.find((pack) => isMasterIni(pack.primaryIniPath)) ??
            namespacePacks[0] ??
            workingPacks[0];
        const hostMasterPath = isMasterIni(host.primaryIniPath) ? host.primaryIniPath : undefined;

        const placement =
            request.placement === "new_folder"
                ? await this.prepareNamespaceNewFolder(node, workingPacks, request, created)
                : {
                      masterDir: hostMasterPath ? path.dirname(hostMasterPath) : host.path,
                      iniRoots: workingPacks
                          .filter((pack) => !hostMasterPath || pack.path !== host.path)
                          .map((pack) => pack.path),
                      existingMasterPath: hostMasterPath,
                      disableRoots: workingPacks.map((pack) => pack.path),
                  };

        const existingChildren = placement.existingMasterPath
            ? await collectNamespaceChildren(placement.existingMasterPath)
            : [];
        const sources = await buildNamespaceSources(
            placement.iniRoots,
            existingChildren,
            node.includeVanilla,
        );
        if (sources.length === 0) throw new Error("NAMESPACE_MERGE_NEEDS_CHILD");

        const masterPath = await writeNamespaceMerge({
            masterDir: placement.masterDir,
            name: sanitizeToken(node.name || request.packName),
            sources,
            forwardKey: node.forwardKey,
            backKey: node.backKey,
            includeVanilla: node.includeVanilla,
            existingMasterPath: placement.existingMasterPath,
            created,
        });
        await disableForeignMasters(placement.disableRoots, masterPath, created);
        return path.dirname(masterPath);
    }

    private async prepareNamespaceNewFolder(
        node: MergePlanGroup,
        packs: MergePackClassification[],
        request: MergeModsRequest,
        created: RollbackAction[],
    ) {
        const masterDir = await this.prepareNewFolder(
            node.name || request.packName,
            request.groupPath,
            created,
        );
        const destNames = assignEnabledFolderNames(
            packs.map((pack) => pack.path),
            await fse.readdir(masterDir),
        );
        const copied = await Promise.all(
            packs.map((pack, index) =>
                copyPackInto(pack.path, masterDir, destNames[index], created),
            ),
        );
        for (const pack of packs) await disableOriginal(pack.path, created);
        return {
            masterDir,
            iniRoots: copied,
            existingMasterPath: undefined,
            disableRoots: [masterDir],
        };
    }

    private async prepareClassicWorkDir(
        node: MergePlanGroup,
        packs: MergePackClassification[],
        request: MergeModsRequest,
        created: RollbackAction[],
    ) {
        const container = await this.prepareNewFolder(
            node.name || request.packName,
            request.groupPath,
            created,
        );
        const destNames = assignEnabledFolderNames(
            packs.map((pack) => pack.path),
            await fse.readdir(container),
        );
        const destPaths = destNames.map((destName) => path.join(container, destName));
        if (request.placement === "new_folder") {
            await Promise.all(
                packs.map((pack, index) =>
                    copyPackInto(pack.path, container, destNames[index], created),
                ),
            );
            for (const pack of packs) await disableOriginal(pack.path, created);
            return destPaths;
        }
        await Promise.all(
            packs.map(async (pack, index) => {
                const dest = destPaths[index];
                await fse.move(pack.path, dest);
                created.push({ kind: "move", from: dest, to: pack.path });
            }),
        );
        return destPaths;
    }

    private async prepareNewFolder(name: string, groupPath: string, created: RollbackAction[]) {
        const existing = await fse.readdir(groupPath);
        const folderName = this.desktop.lib.fs.getUniqueName(sanitizeFolderName(name), existing);
        const target = path.join(groupPath, folderName);
        await fse.ensureDir(target);
        created.push({ kind: "remove", path: target });
        return target;
    }
}

function assertGroupAllowed(node: MergePlanGroup, packs: MergePackClassification[]) {
    if (node.engine !== "classic") return;
    const blocked = packs.find((pack) => !pack.allowsClassic);
    if (blocked) {
        throw new Error(`CLASSIC_LOCKED:${blocked.path}`);
    }
}

async function resolvePrimaryIni(
    mappedPath: string,
    primaryIniPath: string | null,
    originalPath: string,
) {
    if (primaryIniPath) {
        const relative = path.relative(originalPath, primaryIniPath);
        const mapped = path.join(mappedPath, relative);
        if (await fse.pathExists(mapped)) return mapped;
    }
    const inis = await collectEnabledInis(mappedPath);
    return inis[0] ?? null;
}

async function copyPackInto(
    sourcePath: string,
    targetParent: string,
    destName: string,
    created: RollbackAction[],
) {
    const target = path.join(targetParent, destName);
    if (!(await fse.pathExists(target))) {
        await fse.copy(sourcePath, target);
        created.push({ kind: "remove", path: target });
    }
    return target;
}

async function enablePackFolders(packs: MergePackClassification[], created: RollbackAction[]) {
    const reservedByParent = new Map<string, Set<string>>();
    const destPaths: string[] = [];
    for (const pack of packs) {
        const parent = path.dirname(pack.path);
        let used = reservedByParent.get(parent);
        if (!used) {
            used = new Set((await fse.readdir(parent)).map((name) => name.toLowerCase()));
            for (const other of packs) {
                if (path.dirname(other.path) !== parent) continue;
                used.delete(path.basename(other.path).toLowerCase());
            }
            reservedByParent.set(parent, used);
        }
        destPaths.push(path.join(parent, uniqueEnabledFolderName(pack.path, used)));
    }

    const currentPaths = packs.map((pack) => pack.path);
    for (let index = 0; index < packs.length; index += 1) {
        const dest = destPaths[index];
        const current = currentPaths[index];
        if (path.resolve(dest).toLowerCase() === path.resolve(current).toLowerCase()) {
            continue;
        }

        const conflictIndex = currentPaths.findIndex(
            (itemPath, otherIndex) =>
                otherIndex !== index &&
                path.resolve(itemPath).toLowerCase() === path.resolve(dest).toLowerCase(),
        );
        if (conflictIndex !== -1) {
            const conflictSource = currentPaths[conflictIndex];
            const tempPath = await allocateStagePath(conflictSource);
            await fse.move(conflictSource, tempPath);
            created.push({ kind: "move", from: tempPath, to: conflictSource });
            currentPaths[conflictIndex] = tempPath;
        }

        await fse.move(current, dest);
        created.push({ kind: "move", from: dest, to: current });
        currentPaths[index] = dest;
    }

    return packs.map((pack, index) => {
        const finalPath = currentPaths[index];
        return path.resolve(finalPath).toLowerCase() === path.resolve(pack.path).toLowerCase()
            ? pack
            : remapPackPath(pack, finalPath);
    });
}

async function allocateStagePath(sourcePath: string) {
    const parent = path.dirname(sourcePath);
    const baseName = path.basename(sourcePath);
    for (let counter = 1; counter <= 1000; counter += 1) {
        const candidate = path.join(parent, `__nhd_stage_${Date.now()}_${counter}_${baseName}`);
        if (!(await fse.pathExists(candidate))) {
            return candidate;
        }
    }
    throw new Error("STAGE_PATH_CONFLICT");
}

function assignEnabledFolderNames(sourcePaths: string[], reservedNames: string[]) {
    const used = new Set(reservedNames.map((name) => name.toLowerCase()));
    return sourcePaths.map((sourcePath) => uniqueEnabledFolderName(sourcePath, used));
}

function uniqueEnabledFolderName(sourcePath: string, used: Set<string>) {
    const currentName = path.basename(sourcePath);
    const base = stripDisabledPrefix(currentName) || currentName;
    let name = base;
    let counter = 1;
    while (used.has(name.toLowerCase())) {
        counter += 1;
        name = `${base} (${counter})`;
    }
    used.add(name.toLowerCase());
    return name;
}

function remapPackPath(pack: MergePackClassification, nextPath: string): MergePackClassification {
    return {
        ...pack,
        path: nextPath,
        primaryIniPath: pack.primaryIniPath
            ? path.join(nextPath, path.relative(pack.path, pack.primaryIniPath))
            : null,
    };
}

async function disableOriginal(modPath: string, created: RollbackAction[]) {
    const currentName = path.basename(modPath);
    if (/^disabled[\s_]/i.test(currentName)) return;
    const dest = await allocateMergeDisabledPath(modPath);
    await fse.move(modPath, dest);
    created.push({ kind: "move", from: dest, to: modPath });
}

function sanitizeToken(name: string) {
    const token = name.replace(/\s+/g, "");
    if (!isSafeMergeName(token)) throw new Error("MERGE_NAME_REQUIRED");
    return token;
}

function sanitizeFolderName(name: string) {
    return name.trim() || "Merged";
}

function uniquePaths(paths: string[]) {
    return [...new Set(paths.map((entry) => path.resolve(entry)))];
}

async function buildNamespaceSources(
    iniRoots: string[],
    existingChildren: string[],
    includeVanilla: boolean,
) {
    const seen = new Set<string>();
    const take = (iniPath: string) => {
        if (isMasterIni(iniPath)) return false;
        const resolved = path.resolve(iniPath);
        if (seen.has(resolved)) return false;
        seen.add(resolved);
        return true;
    };
    const groups = [
        uniquePaths(existingChildren).filter(take),
        ...(await Promise.all(iniRoots.map((root) => collectEnabledInis(root)))).map((inis) =>
            inis.filter(take),
        ),
    ].filter((group) => group.length > 0);

    return groups.flatMap((group, index) =>
        group.map((iniPath) => ({
            iniPath,
            index: includeVanilla ? index + 1 : index,
        })),
    );
}

function isMasterIni(iniPath: string | null | undefined): iniPath is string {
    if (!iniPath) return false;
    const basename = path.basename(iniPath).toLowerCase();
    return basename.startsWith("master") && basename.endsWith(".ini");
}

async function disableForeignMasters(roots: string[], keepPath: string, created: RollbackAction[]) {
    const keep = path.resolve(keepPath);
    const masters = (await Promise.all(roots.map((root) => collectEnabledInis(root))))
        .flat()
        .filter((iniPath) => isMasterIni(iniPath) && path.resolve(iniPath) !== keep);

    await Promise.all(masters.map((iniPath) => disableIniFile(iniPath, created)));
}
