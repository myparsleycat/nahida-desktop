import path from "node:path";

import { isSafeMergeName } from "@shared/mod";
import type { GameConfig, MergeModsRequest, MergePlacement, MergePlanNode } from "@shared/types";
import { uniq } from "es-toolkit";
import fse from "fs-extra";

import { isSameOrChildPath, normalizeModPath } from "../path-utils";

const MAX_PLAN_DEPTH = 32;
const MAX_PLAN_NODES = 256;

export function parseModPaths(input: unknown): string[] {
    if (!Array.isArray(input) || input.length === 0) throw invalidMergePacks();
    return input.map((entry) => parseAbsolutePath(entry, invalidMergePacks));
}

export function parseMergeModsRequest(input: unknown): MergeModsRequest {
    if (!input || typeof input !== "object") throw invalidMergeRequest();

    const request = input as Record<string, unknown>;
    const groupPath = parseAbsolutePath(request.groupPath, invalidMergeRequest);
    const placement = parsePlacement(request.placement);
    const packName = parsePackName(request.packName);
    const root = parsePlanNode(request.root, 0, { count: 0 });
    if (root.kind !== "group") throw invalidMergeRequest();

    const leaves = collectMergeLeaves(root);
    if (leaves.length < 2) throw invalidMergeRequest();
    if (uniq(leaves.map((leaf) => normalizeModPath(path.resolve(leaf)))).length !== leaves.length) {
        throw invalidMergeRequest();
    }

    return { groupPath, placement, packName, root };
}

export function collectManagedModRoots(
    games: Array<Pick<GameConfig, "modFolderPath" | "linkedModFolderPath">>,
) {
    return games.flatMap((game) =>
        [game.modFolderPath, game.linkedModFolderPath].filter(
            (root): root is string => typeof root === "string" && root.length > 0,
        ),
    );
}

export async function assertOwnedModPaths(paths: string[], roots: string[]) {
    if (roots.length === 0) throw unownedPath();

    const resolvedRoots = await Promise.all(roots.map(resolveForCompare));
    const resolvedPaths = await Promise.all(paths.map(resolveForCompare));
    if (
        resolvedPaths.some(
            (resolved) => !resolvedRoots.some((root) => isSameOrChildPath(root, resolved)),
        )
    ) {
        throw unownedPath();
    }
}

export async function assertMergeRequestPaths(request: MergeModsRequest, roots: string[]) {
    const sourcePaths = collectMergeLeaves(request.root);
    const outputPaths = collectIntendedOutputPaths(request);
    await assertOwnedModPaths([request.groupPath, ...sourcePaths, ...outputPaths], roots);
    await assertWithinGroup(request.groupPath, [...sourcePaths, ...outputPaths]);
}

export function collectMergeLeaves(node: MergePlanNode): string[] {
    if (node.kind === "leaf") return [node.path];
    return node.children.flatMap(collectMergeLeaves);
}

function parsePlanNode(input: unknown, depth: number, state: { count: number }): MergePlanNode {
    state.count += 1;
    if (depth > MAX_PLAN_DEPTH || state.count > MAX_PLAN_NODES) throw invalidMergeRequest();
    if (!input || typeof input !== "object") throw invalidMergeRequest();

    const node = input as Record<string, unknown>;
    if (node.kind === "leaf") {
        return { kind: "leaf", path: parseAbsolutePath(node.path, invalidMergeRequest) };
    }
    if (node.kind !== "group") throw invalidMergeRequest();
    if (typeof node.id !== "string" || !node.id.trim()) throw invalidMergeRequest();
    if (node.engine !== "classic" && node.engine !== "namespace") throw invalidMergeRequest();
    if (typeof node.name !== "string" || !isSafeMergeName(node.name)) {
        throw invalidMergeRequest();
    }
    if (
        typeof node.forwardKey !== "string" ||
        !node.forwardKey.trim() ||
        /[\r\n]/.test(node.forwardKey)
    ) {
        throw invalidMergeRequest();
    }
    if (typeof node.backKey !== "string" || /[\r\n]/.test(node.backKey)) {
        throw invalidMergeRequest();
    }
    if (node.engine === "namespace" && !node.backKey.trim()) throw invalidMergeRequest();
    if (typeof node.includeVanilla !== "boolean") throw invalidMergeRequest();
    if (!Array.isArray(node.children) || node.children.length === 0) throw invalidMergeRequest();

    const children = node.children.map((child) => parsePlanNode(child, depth + 1, state));
    if (children.flatMap(collectMergeLeaves).length < 2) throw invalidMergeRequest();

    return {
        kind: "group",
        id: node.id,
        engine: node.engine,
        name: node.name,
        forwardKey: node.forwardKey,
        backKey: node.backKey,
        includeVanilla: node.includeVanilla,
        children,
    };
}

function parsePlacement(value: unknown): MergePlacement {
    if (value === "in_place" || value === "new_folder") return value;
    throw invalidMergeRequest();
}

function parsePackName(value: unknown) {
    if (typeof value !== "string") throw invalidMergeRequest();
    if (value.trim() !== "" && !isSafeMergeName(value)) throw invalidMergeRequest();
    return value;
}

function parseAbsolutePath(value: unknown, error: () => TypeError) {
    if (typeof value !== "string" || !value.trim() || !path.isAbsolute(value)) {
        throw error();
    }
    return value;
}

function collectIntendedOutputPaths(request: MergeModsRequest) {
    const walk = (node: MergePlanNode): string[] => {
        if (node.kind === "leaf") return [];
        return [
            path.join(request.groupPath, node.name.trim() || request.packName.trim() || "Merged"),
            ...node.children.flatMap(walk),
        ];
    };
    return walk(request.root);
}

async function assertWithinGroup(groupPath: string, paths: string[]) {
    const group = await resolveForCompare(groupPath);
    const resolvedPaths = await Promise.all(paths.map(resolveForCompare));
    if (resolvedPaths.some((resolved) => !isStrictChildPath(group, resolved))) {
        throw outsideGroup();
    }
}

async function resolveForCompare(targetPath: string) {
    const seen = new Set<string>();
    const resolve = async (currentPath: string): Promise<string> => {
        const resolved = path.resolve(currentPath);
        const seenKey = normalizeModPath(resolved);
        if (seen.has(seenKey)) throw unownedPath();
        seen.add(seenKey);

        const stat = await lstatIfPresent(resolved);
        if (stat?.isSymbolicLink()) {
            const target = await fse.readlink(resolved);
            const next = path.isAbsolute(target)
                ? path.resolve(target)
                : path.resolve(path.dirname(resolved), target);
            return await resolve(next);
        }
        if (stat) return await fse.realpath(resolved);

        const parent = path.dirname(resolved);
        if (parent === resolved) return resolved;
        return path.join(await resolve(parent), path.basename(resolved));
    };
    return await resolve(targetPath);
}

async function lstatIfPresent(targetPath: string) {
    try {
        return await fse.lstat(targetPath);
    } catch (error) {
        if ((error as NodeJS.ErrnoException).code === "ENOENT") return null;
        throw error;
    }
}

function isStrictChildPath(parentPath: string, targetPath: string) {
    if (!isSameOrChildPath(parentPath, targetPath)) return false;
    return (
        normalizeModPath(path.resolve(parentPath)) !== normalizeModPath(path.resolve(targetPath))
    );
}

function invalidMergePacks() {
    return new TypeError("Invalid merge pack payload");
}

function invalidMergeRequest() {
    return new TypeError("Invalid merge request payload");
}

function unownedPath() {
    return new TypeError("Path is outside the managed mod folder");
}

function outsideGroup() {
    return new TypeError("Path is outside the selected group");
}
