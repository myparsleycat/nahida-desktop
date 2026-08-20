import { closeSync, openSync, readSync } from "node:fs";
import { open } from "node:fs/promises";
import path from "node:path";

import { decodeDdsToRgba8 } from "@native/mod-tools";
import { analyzePng, parseDdsSrgbState } from "@native/static-glb";
import type { Dnf } from "@shared/mod-viewer/types";
import fse from "fs-extra";
import { PNG } from "pngjs";

import type { ShapeSlider } from "./shapes";

import {
    DNF_TRUE,
    DNF_FALSE,
    buildBoolAliasMap,
    dnfAnd,
    dnfCovers,
    dnfNot,
    dnfOr,
    isUnconstrained,
    normalizeDnf,
    parseConditionDnf,
    sameDnf,
} from "./dnf";
import {
    resourceLookup,
    safeResourcePath,
    sectionLookup,
    stripComment,
    type IniLine,
    type IniSections,
    type ResourceInfo,
} from "./ini";
import { extractToggleKeys, extractVariableDefaults } from "./toggles";

const POSITION_STRIDE = 40;
const DEFAULT_UV_OFFSET = 4;
const RUN_SKIP_PREFIXES = [
    "TextureOverride",
    "ShaderOverride",
    "Resource",
    "Present",
    "Key",
    "Constants",
];
const MAX_RUN_EXPANSIONS = 4096;
const AUX_MAP_CHANNELS: Record<string, "normal_map" | "light_map" | "material_map"> = {
    normalmap: "normal_map",
    lightmap: "light_map",
    materialmap: "material_map",
};
const HASH_TEXTURE_SUFFIX = /(Diffuse|NormalMap|LightMap|MaterialMap)$/i;
const WWMI_DUMP_TEX_RE = /(?:^|[/\\])Components-(\d+(?:-\d+)*)\s+t=/i;
const MAX_HINT_DECODE_BYTES = 32 * 1024 * 1024;
const MAX_HINT_DECODE_AREA = 4096 * 4096;
const IB_COMPONENT_DUMP_RE =
    /(?:^|[/\\])([0-9a-f]{8})_(\d+)_([0-9a-f]{8})_Hash_(DiffuseMap|LightMap|NormalMap|MaterialMap)\./i;
const DDS_SRGB_DXGI = new Set([29, 72, 75, 78, 91, 93, 99]);
const DDS_PACKED_DXGI = new Set([80, 81, 83, 84]);
const DDS_PACKED_FOURCC = new Set(["ATI1", "ATI2", "BC4U", "BC4S", "BC5U", "BC5S"]);

export type TextureAssignment = { res: string; cond: Dnf; authored?: boolean };

export type DrawRecord = {
    label: string;
    count: number | null;
    start: number;
    base: number;
    conditions: Dnf;
    sources: IniLine[];
    ibFile?: string;
    indexSize?: number;
    positionFile?: string;
    texcoordFile?: string;
    positionStride?: number;
    texcoordStride?: number;
    textureDefaultFile?: string;
    textureAuthored?: boolean;
    textureVariants?: Array<{ conditions: Dnf; file: string }>;
    textureAssignments?: Array<{ conditions: Dnf; file: string }>;
    positionVariants?: Array<{ conditions: Dnf; file: string; stride: number }>;
    normalMapDefaultFile?: string;
    normalMapVariants?: Array<{ conditions: Dnf; file: string }>;
    lightMapDefaultFile?: string;
    lightMapVariants?: Array<{ conditions: Dnf; file: string }>;
    materialMapDefaultFile?: string;
    materialMapVariants?: Array<{ conditions: Dnf; file: string }>;
};

export type DrawGroup = {
    name: string;
    displayName: string;
    source?: string;
    positionFile: string;
    texcoordFile: string;
    positionStride: number;
    texcoordStride: number;
    texcoordUvOff: number;
    ibFile: string;
    diffuseFile?: string;
    diffusePoolFiles: Array<{ res: string; file: string }>;
    nonDiffuseTextureFiles: string[];
    indexSize: number;
    draws: DrawRecord[];
    shapeSliders?: ShapeSlider[];
};

type AuxState = {
    variants: TextureAssignment[];
    history: TextureAssignment[];
    chainKey: string | null;
    lastCond: Dnf | null;
};

type SectionInfo = {
    vb0?: string;
    vb1?: string;
    vb2?: string;
    ib?: string;
    draws: DrawSnapshot[];
    diffuse?: string;
    diffusePool: string[];
    src?: IniLine;
    handlingSkip: boolean;
    auxDrawStates: Array<
        Record<string, { variants: TextureAssignment[]; history: TextureAssignment[] }>
    >;
    diffuseVariantsAtEnd: TextureAssignment[];
    diffuseHistoryAtEnd: TextureAssignment[];
    auxMapsAtEnd: Record<string, { variants: TextureAssignment[]; history: TextureAssignment[] }>;
    curIb?: string;
    curVb0?: string;
    curVb1?: string;
    curVb2?: string;
    curDiffuseVariants: TextureAssignment[];
    diffuseChainKey?: string;
    diffuseLastCond?: Dnf;
    nonDiffuseSlotPool: string[];
    diffuseHistory: TextureAssignment[];
    ibHistory: TextureAssignment[];
    vb0History: TextureAssignment[];
    vb1History: TextureAssignment[];
    thisHistory: TextureAssignment[];
    auxMaps: Record<string, AuxState>;
};

type DrawSnapshot = {
    count: number | null;
    start: number;
    base: number;
    conditions: Dnf;
    source?: IniLine;
    ib?: string;
    diffuseVariants: TextureAssignment[];
    diffuseHistory: TextureAssignment[];
    vb: [string | undefined, string | undefined, string | undefined];
};

export function buildDrawGroups(
    sections: IniSections,
    resources: Record<string, ResourceInfo>,
    varPrefix?: string,
    source?: string,
    seen: Record<string, number> = {},
    gatingVars?: Set<string>,
    animation?: { vars: Set<string>; domains: Record<string, string[]> },
    modDir?: string,
): DrawGroup[] {
    const scanVars = gatingVars ?? new Set<string>();
    const secInfo = scanSectionsForDraws(sections, varPrefix, scanVars, animation?.domains);
    const copySources = collectResourceCopySources(sections, resources);
    const resolved = resolveComponentBuffers(secInfo, resources, copySources);
    const drawSecs = selectDrawSections(secInfo, resolved.globalIb);
    if (drawSecs.length === 0) {
        return [];
    }

    const groups: DrawGroup[] = [];
    for (const [secName, info] of drawSecs) {
        const displayName = secName.startsWith("TextureOverride")
            ? secName.slice("TextureOverride".length) || secName
            : secName;
        seen[displayName] = (seen[displayName] ?? 0) + 1;
        const label = seen[displayName] > 1 ? `${displayName}_${seen[displayName]}` : displayName;
        const ibRes = info.ib ?? resolved.globalIb;
        if (!ibRes) {
            continue;
        }
        const component = ibResToComponent(ibRes);
        let buf = lookupCompValue(resolved.componentBuffers, component);
        if (!buf) {
            const position = info.vb0 ?? lookupCompValue(resolved.componentPositions, component);
            const vb2Stride = info.vb2 ? (resourceLookup(resources, info.vb2).stride ?? 0) : 0;
            const texcoord =
                (info.vb2 && vb2Stride !== 32 ? info.vb2 : undefined) ??
                info.vb1 ??
                lookupCompValue(resolved.componentTexcoords, component);
            if (
                position &&
                texcoord &&
                resolveVertexInfo(position, resources, copySources).filename
            ) {
                buf = { position, texcoord };
            }
        }
        if (!buf) {
            const hash = extractHash(secName) ?? extractHash(ibRes);
            if (hash && resolved.hashPositions[hash] && resolved.hashTexcoords[hash]) {
                buf = {
                    position: resolved.hashPositions[hash],
                    texcoord: resolved.hashTexcoords[hash],
                };
            }
        }
        if (!buf && resolved.globalPosition && resolved.globalTexcoord) {
            buf = { position: resolved.globalPosition, texcoord: resolved.globalTexcoord };
        }
        if (!buf) {
            continue;
        }

        const posRi = resolveVertexInfo(buf.position, resources, copySources);
        const tcRi = resourceLookup(resources, buf.texcoord);
        const ibRi = resourceLookup(resources, ibRes);
        const posFile = posRi.filename;
        const tcFile = tcRi.filename;
        const ibFile = ibRi.filename;
        if (!posFile || !tcFile || !ibFile) {
            continue;
        }

        const implicit = implicitDrawsFromIbHistory(info);
        const drawsList = info.draws.length > 0 ? info.draws : implicit.draws;
        const auxDrawStates =
            info.draws.length > 0
                ? info.auxDrawStates
                : implicit.auxDrawStates.length > 0
                  ? implicit.auxDrawStates
                  : [info.auxMapsAtEnd];
        const draws: DrawRecord[] = [];
        for (const [index, snapshot] of drawsList.entries()) {
            const draw: DrawRecord = {
                label: `${label}-${index + 1}`,
                count: snapshot.count,
                start: snapshot.start,
                base: snapshot.base,
                conditions: snapshot.conditions,
                sources: snapshot.source ? [snapshot.source] : [],
            };
            if (snapshot.ib && snapshot.ib !== ibRes) {
                const resolvedIb = resourceLookup(resources, snapshot.ib).filename;
                if (resolvedIb) {
                    draw.ibFile = resolvedIb;
                    draw.indexSize = ibIndexSize(resourceLookup(resources, snapshot.ib).format);
                }
            }
            const drawComponent = ibResToComponent(snapshot.ib ?? ibRes);
            const positionRes =
                pickVariant(
                    lookupCompValue(resolved.componentPositionVariants, drawComponent),
                    snapshot.conditions,
                ) ??
                lookupCompValue(resolved.componentBuffers, drawComponent)?.position ??
                buf.position;
            const texcoordRes =
                pickVariant(
                    lookupCompValue(resolved.componentTexcoordVariants, drawComponent),
                    snapshot.conditions,
                ) ??
                lookupCompValue(resolved.componentBuffers, drawComponent)?.texcoord ??
                buf.texcoord;
            if (positionRes !== buf.position || texcoordRes !== buf.texcoord) {
                const pfile = resolveVertexInfo(positionRes, resources, copySources).filename;
                const tfile = resolveVertexInfo(texcoordRes, resources, copySources).filename;
                if (pfile && tfile) {
                    draw.positionFile = pfile;
                    draw.texcoordFile = tfile;
                    draw.positionStride =
                        resolveVertexInfo(positionRes, resources, copySources).stride ??
                        POSITION_STRIDE;
                    draw.texcoordStride =
                        resolveVertexInfo(texcoordRes, resources, copySources).stride ?? 20;
                }
            }
            const positionAssignments = (
                lookupCompValue(resolved.componentPositionVariants, drawComponent) ?? []
            )
                .map((assignment) => {
                    const info = resolveVertexInfo(assignment.res, resources, copySources);
                    return info.filename
                        ? {
                              conditions: assignment.cond,
                              file: info.filename,
                              stride: info.stride ?? POSITION_STRIDE,
                          }
                        : null;
                })
                .filter((entry): entry is NonNullable<typeof entry> => !!entry);
            if (new Set(positionAssignments.map((entry) => entry.file)).size > 1) {
                draw.positionVariants = positionAssignments;
            }

            const coveredDiffuse =
                snapshot.diffuseVariants.length > 0
                    ? snapshot.diffuseVariants
                    : info.diffuseHistory.filter((entry) =>
                          dnfCovers(snapshot.conditions, entry.cond),
                      );
            const assignedDiffuse =
                coveredDiffuse.length > 0 ? coveredDiffuse : info.diffuseHistory;
            const variants = assignedDiffuse
                .map((entry) => {
                    const file = resourceLookup(resources, entry.res).filename;
                    return file ? { conditions: entry.cond, file } : null;
                })
                .filter((entry): entry is { conditions: Dnf; file: string } => !!entry);
            if (variants.length > 0) {
                draw.textureDefaultFile = variants[0].file;
                draw.textureAuthored = assignedDiffuse.some((entry) => entry.authored);
            } else {
                const fallback = lookupRoleResource(snapshot.ib ?? ibRes, "Diffuse", resources);
                const file = fallback ? resourceLookup(resources, fallback).filename : undefined;
                if (file) {
                    draw.textureDefaultFile = file;
                    draw.textureAuthored = true;
                }
            }
            if (variants.length > 1) {
                draw.textureVariants = variants;
            }
            const history = snapshot.diffuseHistory
                .map((entry) => {
                    const file = resourceLookup(resources, entry.res).filename;
                    return file ? { conditions: entry.cond, file } : null;
                })
                .filter((entry): entry is { conditions: Dnf; file: string } => !!entry);
            const legacyVars = new Set(
                variants.flatMap((entry) =>
                    entry.conditions.flatMap((group) => group.map((clause) => clause.var)),
                ),
            );
            const historyVars = new Set(
                history.flatMap((entry) =>
                    entry.conditions.flatMap((group) => group.map((clause) => clause.var)),
                ),
            );
            if (
                history.length > 1 &&
                ([...historyVars].some((variable) => !legacyVars.has(variable)) ||
                    history.length > variants.length)
            ) {
                draw.textureAssignments = history;
            }

            const auxState = auxDrawStates[Math.min(index, auxDrawStates.length - 1)] ?? {};
            for (const [channel, state] of Object.entries(auxState)) {
                const authored = state.history.length > 0 ? state.history : state.variants;
                const resolvedMaps: Array<{ conditions: Dnf; file: string }> = [];
                let defaultFile: string | undefined;
                for (const assignment of authored) {
                    const file = resourceLookup(resources, assignment.res).filename;
                    if (!file) {
                        continue;
                    }
                    resolvedMaps.push({ conditions: assignment.cond, file });
                    if (isUnconstrained(assignment.cond)) {
                        defaultFile = file;
                    }
                }
                if (defaultFile) {
                    if (channel === "normal_map") {
                        draw.normalMapDefaultFile = defaultFile;
                    } else if (channel === "light_map") {
                        draw.lightMapDefaultFile = defaultFile;
                    } else {
                        draw.materialMapDefaultFile = defaultFile;
                    }
                }
                if (
                    resolvedMaps.length > 1 ||
                    (resolvedMaps[0] && !isUnconstrained(resolvedMaps[0].conditions))
                ) {
                    if (channel === "normal_map") {
                        draw.normalMapVariants = resolvedMaps;
                    } else if (channel === "light_map") {
                        draw.lightMapVariants = resolvedMaps;
                    } else {
                        draw.materialMapVariants = resolvedMaps;
                    }
                }
            }
            if (!draw.lightMapDefaultFile && !draw.lightMapVariants) {
                const fallback = lookupRoleResource(snapshot.ib ?? ibRes, "LightMap", resources);
                const file = fallback ? resourceLookup(resources, fallback).filename : undefined;
                if (file) {
                    draw.lightMapDefaultFile = file;
                }
            }
            draws.push(draw);
        }

        const poolFiles: Array<{ res: string; file: string }> = [];
        const seenPool = new Set<string>();
        for (const res of info.diffusePool) {
            const file = resourceLookup(resources, res).filename;
            if (file && !seenPool.has(file)) {
                seenPool.add(file);
                poolFiles.push({ res, file });
            }
        }

        groups.push({
            name: label,
            displayName,
            source,
            positionFile: posFile,
            texcoordFile: tcFile,
            positionStride: posRi.stride ?? POSITION_STRIDE,
            texcoordStride: tcRi.stride ?? 20,
            texcoordUvOff: DEFAULT_UV_OFFSET,
            ibFile,
            diffuseFile: info.diffuse
                ? resourceLookup(resources, info.diffuse).filename
                : undefined,
            diffusePoolFiles: poolFiles,
            nonDiffuseTextureFiles: [
                ...new Set(
                    info.nonDiffuseSlotPool.flatMap((res) => {
                        const file = resourceLookup(resources, res).filename;
                        return file ? [file] : [];
                    }),
                ),
            ],
            indexSize: ibIndexSize(ibRi.format),
            draws,
        });
    }
    const collapsed = collapseAnimationDraws(groups, animation?.vars);
    attachIbComponentDumpTextures(collapsed, resources);
    bindHashImageTextures(collapsed, secInfo, resources, modDir);
    return collapsed;
}

export async function attachWwmiDumpTextures(
    groups: DrawGroup[],
    resources: Record<string, ResourceInfo>,
    modDir: string,
): Promise<void> {
    const targets = groups.filter(
        (group) => wwmiComponentIndex(group) && group.draws.some((draw) => !draw.textureAuthored),
    );
    if (targets.length === 0) {
        return;
    }

    const hintCache = new Map<string, Promise<WwmiTextureHint | null>>();
    const inspect = (relative: string | undefined) => {
        const resolved = safeResourcePath(modDir, relative);
        if (!resolved) {
            return Promise.resolve(null);
        }
        const cached = hintCache.get(resolved);
        if (cached) {
            return cached;
        }
        const pending = inspectWwmiTextureHint(resolved).catch(() => null);
        hintCache.set(resolved, pending);
        return pending;
    };

    const needsDump = new Set<string>();
    for (const group of targets) {
        const index = wwmiComponentIndex(group);
        if (!index) {
            continue;
        }
        for (const draw of group.draws) {
            if (draw.textureAuthored) {
                continue;
            }
            const hint = await inspect(draw.textureDefaultFile);
            if (!hint || !isLikelyWwmiDiffuse(hint)) {
                needsDump.add(index);
            }
        }
    }

    const excludedByIndex = targets.reduce((map, group) => {
        const index = wwmiComponentIndex(group);
        if (!index || !needsDump.has(index)) {
            return map;
        }
        return map.set(
            index,
            new Set([...(map.get(index) ?? []), ...group.nonDiffuseTextureFiles]),
        );
    }, new Map<string, Set<string>>());

    const filesByIndex = Object.values(resources).reduce((map, info) => {
        const file = info.filename;
        const indices = file
            ? WWMI_DUMP_TEX_RE.exec(file.replaceAll("\\", "/"))?.[1]?.split("-")
            : undefined;
        if (!file || !indices) {
            return map;
        }
        for (const index of indices) {
            if (!needsDump.has(index) || excludedByIndex.get(index)?.has(file)) {
                continue;
            }
            map.set(index, [...(map.get(index) ?? []), file]);
        }
        return map;
    }, new Map<string, string[]>());

    const pickedByIndex = new Map(
        (
            await Promise.all(
                [...filesByIndex].map(async ([index, files]) => {
                    const scored = (
                        await Promise.all(
                            files.map(async (file, order) => {
                                const hint = await inspect(file);
                                if (!hint || !isLikelyWwmiDiffuse(hint)) {
                                    return null;
                                }
                                return {
                                    file,
                                    order,
                                    srgb: hint.srgb,
                                    area: hint.area,
                                    bytes: hint.bytes,
                                };
                            }),
                        )
                    ).filter((entry): entry is NonNullable<typeof entry> => !!entry);
                    const picked = pickWwmiDumpDiffuse(scored);
                    return picked ? ([index, picked] as const) : null;
                }),
            )
        ).filter((entry): entry is NonNullable<typeof entry> => !!entry),
    );

    for (const group of targets) {
        const dumpPick = pickedByIndex.get(wwmiComponentIndex(group) ?? "");
        for (const draw of group.draws) {
            if (draw.textureAuthored) {
                continue;
            }
            const currentHint = await inspect(draw.textureDefaultFile);
            if (currentHint && isLikelyWwmiDiffuse(currentHint)) {
                draw.textureVariants = await keepLikelyDiffuseVariants(
                    draw.textureVariants,
                    inspect,
                );
                draw.textureAssignments = await keepLikelyDiffuseVariants(
                    draw.textureAssignments,
                    inspect,
                );
                continue;
            }
            if (!dumpPick) {
                continue;
            }
            draw.textureDefaultFile = dumpPick;
            group.diffuseFile ??= dumpPick;
            draw.textureVariants = undefined;
            draw.textureAssignments = undefined;
        }
    }
}

export function attachIbComponentDumpTextures(
    groups: DrawGroup[],
    resources: Record<string, ResourceInfo>,
): void {
    const dumps = Object.values(resources).flatMap((info) => {
        const file = info.filename;
        const match = file ? IB_COMPONENT_DUMP_RE.exec(file.replaceAll("\\", "/")) : undefined;
        if (!file || !match) {
            return [];
        }
        return [
            {
                file,
                ibHash: match[1].toLowerCase(),
                index: match[2],
                role: match[4].toLowerCase(),
            },
        ];
    });
    if (dumps.length === 0) {
        return;
    }

    for (const group of groups) {
        const ref = ibComponentDumpRef(group);
        if (!ref) {
            continue;
        }
        const ibDumps = dumps.filter((entry) => entry.ibHash === ref.ibHash);
        const diffuse = pickIbComponentDumpFile(ibDumps, ref.index, "diffusemap");
        const light = pickIbComponentDumpFile(ibDumps, ref.index, "lightmap");
        const normal = pickIbComponentDumpFile(ibDumps, ref.index, "normalmap");
        const material = pickIbComponentDumpFile(ibDumps, ref.index, "materialmap");
        group.diffuseFile ??= diffuse;
        for (const draw of group.draws) {
            draw.textureDefaultFile ??= diffuse;
            draw.lightMapDefaultFile ??= light;
            draw.normalMapDefaultFile ??= normal;
            draw.materialMapDefaultFile ??= material;
        }
    }
}

function ibComponentDumpRef(group: DrawGroup): { ibHash: string; index: string } | undefined {
    const match = /(?:^|_)IB_([0-9a-f]{8})(?:_[A-Za-z][A-Za-z0-9]*)*_Component(\d+)$/i.exec(
        group.displayName || group.name,
    );
    if (!match) {
        return undefined;
    }
    return { ibHash: match[1].toLowerCase(), index: match[2] };
}

function pickIbComponentDumpFile(
    dumps: Array<{ file: string; index: string; role: string }>,
    index: string,
    role: string,
): string | undefined {
    const roleDumps = dumps.filter((entry) => entry.role === role);
    const indexed = roleDumps.find((entry) => entry.index === index)?.file;
    if (indexed) {
        return indexed;
    }
    const unique = [...new Set(roleDumps.map((entry) => entry.file))];
    return unique.length === 1 ? unique[0] : undefined;
}

type HashImageSlot = {
    files: Array<{ file: string; cond: Dnf }>;
    role: "diffuse" | "light_map";
    vars: Set<string>;
    area: number;
};

function bindHashImageTextures(
    groups: DrawGroup[],
    secInfo: Record<string, SectionInfo>,
    resources: Record<string, ResourceInfo>,
    modDir?: string,
): void {
    const needed = groups.filter(
        (group) =>
            ibComponentDumpRef(group) && group.draws.some((draw) => !draw.textureDefaultFile),
    );
    if (needed.length === 0) {
        return;
    }

    const slots = Object.entries(secInfo).flatMap(([name, info]) => {
        if (
            !name.startsWith("TextureOverride") ||
            HASH_TEXTURE_SUFFIX.test(name) ||
            info.ib ||
            info.draws.length > 0
        ) {
            return [];
        }
        const files = info.thisHistory.flatMap((entry) => {
            const file = resourceLookup(resources, entry.res).filename;
            if (!file || !/\.(dds|png|jpe?g)$/i.test(file)) {
                return [];
            }
            return [{ file, cond: entry.cond }];
        });
        if (files.length === 0) {
            return [];
        }
        const hint = hashImageHint(files[0].file, modDir);
        return [
            {
                files,
                role: hint.role,
                vars: new Set(files.flatMap((entry) => dnfVars(entry.cond))),
                area: hint.area,
            } satisfies HashImageSlot,
        ];
    });
    if (slots.length === 0) {
        return;
    }

    const diffuseSlots = slots.filter((slot) => slot.role === "diffuse");
    const lightsByArea = slots
        .filter((slot) => slot.role === "light_map")
        .reduce((map, slot) => {
            const list = map.get(slot.area) ?? [];
            list.push(slot);
            return map.set(slot.area, list);
        }, new Map<number, HashImageSlot[]>());
    const paired = new Map(
        diffuseSlots.map((slot) => {
            const lights = lightsByArea.get(slot.area) ?? [];
            const light = slot.area > 0 ? lights.shift() : undefined;
            return [slot, light] as const;
        }),
    );

    const byIb = needed.reduce((map, group) => {
        const ibHash = ibComponentDumpRef(group)?.ibHash;
        if (!ibHash) {
            return map;
        }
        return map.set(ibHash, [...(map.get(ibHash) ?? []), group]);
    }, new Map<string, DrawGroup[]>());
    const unused = new Set(diffuseSlots);
    const unbound = new Set(byIb.keys());

    const scored = [...unbound].flatMap((ibHash) => {
        const groupVars = new Set(
            (byIb.get(ibHash) ?? []).flatMap((group) =>
                group.draws.flatMap((draw) => dnfVars(draw.conditions)),
            ),
        );
        return [...unused].flatMap((slot) => {
            const score = overlapScore(groupVars, slot.vars);
            return score > 0 ? [{ ibHash, slot, score, tightness: slot.vars.size }] : [];
        });
    });
    for (const pick of [...scored].sort(
        (left, right) =>
            right.score - left.score ||
            left.tightness - right.tightness ||
            right.slot.area - left.slot.area,
    )) {
        if (!unbound.has(pick.ibHash) || !unused.has(pick.slot)) {
            continue;
        }
        applyHashImageSlot(byIb.get(pick.ibHash) ?? [], pick.slot, paired.get(pick.slot));
        unbound.delete(pick.ibHash);
        unused.delete(pick.slot);
    }

    const leftoverIbs = [...unbound].sort(
        (left, right) => ibDrawWeight(byIb.get(right) ?? []) - ibDrawWeight(byIb.get(left) ?? []),
    );
    const leftoverSlots = [...unused].sort((left, right) => right.area - left.area || 0);
    for (const [index, ibHash] of leftoverIbs.entries()) {
        const slot = leftoverSlots[index];
        if (slot) {
            applyHashImageSlot(byIb.get(ibHash) ?? [], slot, paired.get(slot));
        }
    }
}

function applyHashImageSlot(groups: DrawGroup[], slot: HashImageSlot, light?: HashImageSlot): void {
    const defaultFile =
        slot.files.find((entry) => isUnconstrained(entry.cond))?.file ?? slot.files[0]?.file;
    const variants = slot.files.map((entry) => ({ conditions: entry.cond, file: entry.file }));
    const keepVariants =
        variants.length > 1 || (variants[0] && !isUnconstrained(variants[0].conditions));
    const lightFile = light?.files[0]?.file;
    for (const group of groups) {
        group.diffuseFile ??= defaultFile;
        for (const draw of group.draws) {
            draw.textureDefaultFile ??= defaultFile;
            if (keepVariants && !draw.textureVariants?.length) {
                draw.textureVariants = variants;
            }
            if (lightFile) {
                draw.lightMapDefaultFile ??= lightFile;
            }
        }
    }
}

function hashImageHint(
    relative: string,
    modDir?: string,
): { role: "diffuse" | "light_map"; area: number } {
    const lower = relative.replaceAll("\\", "/").toLowerCase();
    if (/hash_lightmap|_lightmap\./i.test(lower) && !/hash_diffusemap|diffuse/i.test(lower)) {
        return { role: "light_map", area: peekImageArea(relative, modDir) };
    }
    if (/hash_diffusemap|diffuse/i.test(lower) || !lower.endsWith(".dds") || !modDir) {
        return { role: "diffuse", area: peekImageArea(relative, modDir) };
    }
    const peeked = peekDdsRole(safeResourcePath(modDir, relative));
    return peeked ?? { role: "diffuse", area: 0 };
}

function peekImageArea(relative: string, modDir?: string): number {
    if (!modDir || !relative.toLowerCase().endsWith(".dds")) {
        return 0;
    }
    return peekDdsRole(safeResourcePath(modDir, relative))?.area ?? 0;
}

function peekDdsRole(
    filePath: string | null,
): { role: "diffuse" | "light_map"; area: number } | null {
    if (!filePath) {
        return null;
    }
    try {
        const header = readFileSyncPrefix(filePath, 148);
        if (header.length < 128 || header.toString("ascii", 0, 4) !== "DDS ") {
            return null;
        }
        const area = header.readUInt32LE(16) * header.readUInt32LE(12);
        if (header.toString("ascii", 84, 88) !== "DX10" || header.length < 132) {
            return { role: "diffuse", area };
        }
        return {
            role: DDS_SRGB_DXGI.has(header.readUInt32LE(128)) ? "diffuse" : "light_map",
            area,
        };
    } catch {
        return null;
    }
}

function readFileSyncPrefix(filePath: string, length: number): Buffer {
    const fd = openSync(filePath, "r");
    try {
        const header = Buffer.alloc(length);
        const read = readSync(fd, header, 0, length, 0);
        return header.subarray(0, read);
    } finally {
        closeSync(fd);
    }
}

function dnfVars(dnf: Dnf): string[] {
    return dnf.flatMap((group) => group.map((clause) => clause.var));
}

function overlapScore(groupVars: Set<string>, slotVars: Set<string>): number {
    return [...slotVars].filter((slotVar) =>
        [...groupVars].some(
            (groupVar) =>
                groupVar === slotVar ||
                groupVar.startsWith(slotVar) ||
                slotVar.startsWith(groupVar),
        ),
    ).length;
}

function ibDrawWeight(groups: DrawGroup[]): number {
    return groups.reduce(
        (sum, group) => sum + group.draws.reduce((inner, draw) => inner + (draw.count ?? 0), 0),
        0,
    );
}

export type WwmiTextureHint = {
    srgb: boolean;
    colorSpace: "srgb" | "linear" | "unknown";
    area: number;
    bytes: number;
    isLikelyFlat: boolean;
    isLikelyNormal: boolean;
    isLikelyPacked: boolean;
};

export function isLikelyWwmiDiffuse(hint: WwmiTextureHint): boolean {
    if (hint.colorSpace === "linear") {
        return false;
    }
    return !hint.isLikelyFlat && !hint.isLikelyNormal && !hint.isLikelyPacked;
}

export function pickWwmiDumpDiffuse(
    candidates: Array<{ file: string; srgb: boolean; area: number; bytes: number; order: number }>,
): string | undefined {
    const ranked = [...candidates].sort(
        (left, right) =>
            wwmiDumpShareCount(left.file) - wwmiDumpShareCount(right.file) ||
            Number(right.srgb) - Number(left.srgb) ||
            right.area - left.area ||
            right.bytes - left.bytes ||
            left.order - right.order,
    );
    return ranked[0]?.file;
}

function wwmiDumpShareCount(file: string): number {
    return WWMI_DUMP_TEX_RE.exec(file.replaceAll("\\", "/"))?.[1]?.split("-").length || 1;
}

async function keepLikelyDiffuseVariants(
    variants: Array<{ conditions: Dnf; file: string }> | undefined,
    inspect: (relative: string | undefined) => Promise<WwmiTextureHint | null>,
) {
    if (!variants?.length) {
        return variants;
    }
    const kept = (
        await Promise.all(
            variants.map(async (variant) => {
                const hint = await inspect(variant.file);
                return hint && isLikelyWwmiDiffuse(hint) ? variant : null;
            }),
        )
    ).filter((variant): variant is NonNullable<typeof variant> => !!variant);
    return kept.length > 0 ? kept : undefined;
}

async function inspectWwmiTextureHint(filePath: string): Promise<WwmiTextureHint | null> {
    if (!(await fse.pathExists(filePath))) {
        return null;
    }
    const bytes = (await fse.stat(filePath)).size;
    const ext = path.extname(filePath).toLowerCase();
    if (ext === ".png") {
        if (bytes > MAX_HINT_DECODE_BYTES) {
            return {
                srgb: true,
                colorSpace: "srgb",
                area: 0,
                bytes,
                isLikelyFlat: false,
                isLikelyNormal: false,
                isLikelyPacked: false,
            };
        }
        const png = PNG.sync.read(await fse.readFile(filePath));
        return hintFromAnalysis(
            analyzePng(png.data, png.width, png.height),
            "srgb",
            png.width * png.height,
            bytes,
        );
    }
    if (ext === ".jpg" || ext === ".jpeg") {
        return {
            srgb: true,
            colorSpace: "srgb",
            area: 0,
            bytes,
            isLikelyFlat: false,
            isLikelyNormal: false,
            isLikelyPacked: false,
        };
    }
    if (ext !== ".dds" || bytes < 128) {
        return {
            srgb: false,
            colorSpace: "unknown",
            area: 0,
            bytes,
            isLikelyFlat: false,
            isLikelyNormal: false,
            isLikelyPacked: false,
        };
    }

    const header = await readFilePrefix(filePath, Math.min(148, bytes));
    if (header.length < 128) {
        return {
            srgb: false,
            colorSpace: "unknown",
            area: 0,
            bytes,
            isLikelyFlat: false,
            isLikelyNormal: false,
            isLikelyPacked: false,
        };
    }
    const area = header.readUInt32LE(16) * header.readUInt32LE(12);
    const fourcc = header.toString("ascii", 84, 88);
    const dxgi = fourcc === "DX10" && header.length >= 132 ? header.readUInt32LE(128) : -1;
    const srgbState = parseDdsSrgbState(header);
    const colorSpace = srgbState === true ? "srgb" : srgbState === false ? "linear" : "unknown";
    const packedFormat = DDS_PACKED_FOURCC.has(fourcc) || DDS_PACKED_DXGI.has(dxgi);
    if (colorSpace === "linear" || packedFormat || area > MAX_HINT_DECODE_AREA) {
        return {
            srgb: colorSpace === "srgb",
            colorSpace,
            area,
            bytes,
            isLikelyFlat: false,
            isLikelyNormal: packedFormat,
            isLikelyPacked: packedFormat,
        };
    }
    const decoded = await decodeDdsToRgba8(filePath);
    return hintFromAnalysis(
        analyzePng(decoded.data, decoded.width, decoded.height),
        colorSpace,
        area,
        bytes,
    );
}

function hintFromAnalysis(
    analysis: {
        channelRangeMax: number;
        luminanceStdDev: number;
        meanR: number;
        meanG: number;
        meanB: number;
        blueDominance: number;
    },
    colorSpace: WwmiTextureHint["colorSpace"],
    area: number,
    bytes: number,
): WwmiTextureHint {
    const means = [analysis.meanR, analysis.meanG, analysis.meanB];
    return {
        srgb: colorSpace === "srgb",
        colorSpace,
        area,
        bytes,
        isLikelyFlat:
            analysis.channelRangeMax <= 12 ||
            (analysis.luminanceStdDev <= 0.035 && analysis.channelRangeMax <= 24) ||
            analysis.luminanceStdDev <= 0.012,
        isLikelyNormal:
            analysis.meanB >= 0.7 &&
            Math.abs(analysis.meanR - 0.5) <= 0.18 &&
            Math.abs(analysis.meanG - 0.5) <= 0.18 &&
            analysis.blueDominance >= 0.12 &&
            analysis.channelRangeMax <= 72 &&
            analysis.luminanceStdDev <= 0.12,
        isLikelyPacked:
            means.some((value) => value <= 0.04) &&
            means.some((value) => Math.abs(value - 0.5) <= 0.15),
    };
}

export function attachShapeSliders(groups: DrawGroup[], shapeSliders: ShapeSlider[]): void {
    const pathKey = (file?: string) => file?.replaceAll("\\", "/").toLowerCase();
    for (const group of groups) {
        const positionFiles = new Set(
            [
                pathKey(group.positionFile),
                ...group.draws.map((draw) => pathKey(draw.positionFile)),
            ].filter((value): value is string => !!value),
        );
        const matches = shapeSliders.filter((slider) =>
            positionFiles.has(pathKey(slider.baseFile) ?? ""),
        );
        if (matches.length > 0) {
            group.shapeSliders = matches;
        }
    }
}

function wwmiComponentIndex(group: DrawGroup): string | undefined {
    return (
        /^Component(\d+)$/i.exec(group.displayName)?.[1] ??
        /^Component(\d+)$/i.exec(group.name)?.[1]
    );
}

async function readFilePrefix(filePath: string, length: number): Promise<Buffer> {
    const handle = await open(filePath, "r");
    const header = Buffer.alloc(length);
    const read = await handle.read(header, 0, length, 0).finally(() => handle.close());
    return header.subarray(0, read.bytesRead);
}

function scanSectionsForDraws(
    sections: IniSections,
    varPrefix: string | undefined,
    toggleVars: Set<string>,
    animationDomains?: Record<string, string[]>,
): Record<string, SectionInfo> {
    const aliasMap = buildBoolAliasMap(sections);
    const toggleKeys = extractToggleKeys(sections);
    const valueDomains = {
        ...toggleValueDomainsFromKeys(toggleKeys),
        ...animationDomains,
    };
    const constants = extractVariableDefaults(sections);
    const trackedVars = new Set(toggleVars);
    for (const info of Object.values(toggleKeys)) {
        for (const variable of Object.keys(info.vars)) {
            trackedVars.add(variable);
        }
    }
    let seqCounter = 0;
    let bareCounter = 0;
    let runExpansions = 0;
    const auxBareCounters: Record<string, number> = {
        normal_map: 0,
        light_map: 0,
        material_map: 0,
    };

    const stackedCond = (condStack: Array<{ cur: Dnf }>) => {
        let combined = DNF_TRUE;
        for (const frame of condStack) {
            combined = dnfAnd(combined, frame.cur);
        }
        return normalizeDnf(combined, trackedVars, varPrefix);
    };

    const scan = (
        lines: IniLine[],
        info: SectionInfo,
        condStack: Array<{ cur: Dnf; seen: Dnf; seq: number }>,
        visiting: Set<string>,
        sectionName: string,
    ) => {
        const sectionRole = HASH_TEXTURE_SUFFIX.exec(sectionName)?.[1];
        for (const raw of lines) {
            const line = stripComment(raw.text);
            if (!line) {
                continue;
            }
            info.src ??= raw;
            const lowered = line.toLowerCase();
            const elif = /^(?:else\s+if|elif)\s+(.*)$/i.exec(line);
            if (elif) {
                if (condStack.length > 0) {
                    const frame = condStack[condStack.length - 1];
                    const branch = parseConditionDnf(elif[1].trim(), aliasMap, valueDomains);
                    frame.cur = dnfAnd(dnfNot(frame.seen), branch);
                    frame.seen = dnfOr(frame.seen, branch);
                }
                continue;
            }
            if (lowered.startsWith("if ")) {
                const branch = parseConditionDnf(line.slice(3).trim(), aliasMap, valueDomains);
                seqCounter += 1;
                condStack.push({ cur: branch, seen: branch, seq: seqCounter });
                continue;
            }
            if (lowered === "else") {
                if (condStack.length > 0) {
                    condStack[condStack.length - 1].cur = dnfNot(
                        condStack[condStack.length - 1].seen,
                    );
                }
                continue;
            }
            if (lowered === "endif") {
                condStack.pop();
                continue;
            }

            const vb0 = /^vb0\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
            if (vb0) {
                const value = vb0[1].toLowerCase() === "null" ? undefined : vb0[1];
                const cond = stackedCond(condStack);
                if (!sameDnf(cond, DNF_FALSE)) {
                    info.vb0 ??= value;
                    info.curVb0 = value;
                }
                if (value) {
                    info.vb0History.push({ res: value, cond });
                }
            }
            const vb1 = /^vb1\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
            if (vb1) {
                const value = vb1[1].toLowerCase() === "null" ? undefined : vb1[1];
                const cond = stackedCond(condStack);
                if (!sameDnf(cond, DNF_FALSE)) {
                    info.vb1 ??= value;
                    info.curVb1 = value;
                }
                if (value) {
                    info.vb1History.push({ res: value, cond });
                }
            }
            const vb2 = /^vb2\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
            if (vb2) {
                const value = vb2[1].toLowerCase() === "null" ? undefined : vb2[1];
                const cond = stackedCond(condStack);
                if (!sameDnf(cond, DNF_FALSE)) {
                    info.vb2 ??= value;
                    info.curVb2 = value;
                }
            }
            const ib = /^ib\s*=\s*(\S+)/i.exec(line);
            if (ib) {
                const cond = stackedCond(condStack);
                if (!sameDnf(cond, DNF_FALSE)) {
                    info.ib ??= ib[1];
                    info.curIb = ib[1];
                }
                info.ibHistory.push({ res: ib[1], cond });
            }
            if (/^handling\s*=\s*skip\b/i.test(line)) {
                info.handlingSkip = true;
            }
            const draw = /^drawindexed\s*=\s*([^,]+)\s*,\s*([^,]+)\s*,\s*([^,]+)/i.exec(line);
            if (draw) {
                const count = resolveDrawNumber(draw[1], constants);
                const start = resolveDrawNumber(draw[2], constants);
                const base = resolveDrawNumber(draw[3], constants);
                if (count !== null && start !== null && base !== null) {
                    info.draws.push({
                        count,
                        start,
                        base,
                        conditions: stackedCond(condStack),
                        source: raw,
                        ib: info.curIb,
                        diffuseVariants: [...info.curDiffuseVariants],
                        diffuseHistory: [...info.diffuseHistory],
                        vb: [info.curVb0, info.curVb1, info.curVb2],
                    });
                    info.auxDrawStates.push(auxSnapshot(info));
                }
            }

            const selfAssign = /^this\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
            if (selfAssign && selfAssign[1].toLowerCase() !== "null") {
                info.thisHistory.push({ res: selfAssign[1], cond: stackedCond(condStack) });
            }

            let diffuse = /^Resource\\[^\\]+\\Diffuse\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
            let authoredDiffuse = Boolean(diffuse);
            if (!diffuse) {
                const ps = /^ps-t(\d+)\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
                if (ps && ps[1] !== "0" && !/Diffuse/i.test(ps[2])) {
                    info.nonDiffuseSlotPool.push(ps[2]);
                }
                if (
                    ps &&
                    !/(NormalMap|LightMap|MaterialMap)/i.test(ps[2]) &&
                    (ps[1] === "0" || /Diffuse/i.test(ps[2]))
                ) {
                    diffuse = [ps[0], ps[2]] as unknown as RegExpExecArray;
                    authoredDiffuse = /Diffuse/i.test(ps[2]);
                }
            }
            if (!diffuse) {
                const self = /^this\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
                if (self && (/Diffuse/i.test(self[1]) || /^Diffuse$/i.test(sectionRole ?? ""))) {
                    diffuse = self;
                    authoredDiffuse = true;
                }
            }
            if (diffuse) {
                const res = diffuse[1];
                info.diffuse ??= res;
                if (!info.diffusePool.includes(res)) {
                    info.diffusePool.push(res);
                }
                let combined = DNF_TRUE;
                for (const frame of condStack) {
                    combined = dnfAnd(combined, frame.cur);
                }
                const cond = normalizeDnf(combined, trackedVars, varPrefix);
                const chainKey =
                    condStack.length > 0
                        ? `if:${condStack[condStack.length - 1].seq}`
                        : `bare:${++bareCounter}`;
                if (chainKey !== info.diffuseChainKey) {
                    info.curDiffuseVariants = [];
                    info.diffuseChainKey = chainKey;
                } else if (
                    info.diffuseLastCond &&
                    jsonDnf(info.diffuseLastCond) === jsonDnf(cond) &&
                    info.curDiffuseVariants.length > 0
                ) {
                    info.curDiffuseVariants.pop();
                }
                info.curDiffuseVariants.push({ res, cond, authored: authoredDiffuse });
                info.diffuseLastCond = cond;
                info.diffuseHistory.push({ res, cond, authored: authoredDiffuse });
            }

            let aux =
                /^Resource\\[^\\]+\\(NormalMap|LightMap|MaterialMap)\s*=\s*(?:ref\s+)?(\S+)/i.exec(
                    line,
                );
            if (!aux) {
                const direct = /^(?:ps-t\d+|this)\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
                const role = direct ? /(NormalMap|LightMap|MaterialMap)/i.exec(direct[1]) : null;
                if (direct && role) {
                    aux = [direct[0], role[1], direct[1]] as unknown as RegExpExecArray;
                } else if (
                    direct &&
                    /^this\s*=/i.test(line) &&
                    sectionRole &&
                    !/^Diffuse$/i.test(sectionRole)
                ) {
                    aux = [direct[0], sectionRole, direct[1]] as unknown as RegExpExecArray;
                }
            }
            if (aux) {
                const channel = AUX_MAP_CHANNELS[aux[1].toLowerCase()];
                const res = aux[2];
                const state = (info.auxMaps[channel] ??= {
                    variants: [],
                    history: [],
                    chainKey: null,
                    lastCond: null,
                });
                let combined = DNF_TRUE;
                for (const frame of condStack) {
                    combined = dnfAnd(combined, frame.cur);
                }
                const cond = normalizeDnf(combined, trackedVars, varPrefix);
                const chainKey =
                    condStack.length > 0
                        ? `if:${condStack[condStack.length - 1].seq}`
                        : `bare:${channel}:${++auxBareCounters[channel]}`;
                if (chainKey !== state.chainKey) {
                    state.variants = [];
                    state.chainKey = chainKey;
                } else if (
                    state.lastCond &&
                    jsonDnf(state.lastCond) === jsonDnf(cond) &&
                    state.variants.length > 0
                ) {
                    state.variants.pop();
                }
                state.variants.push({ res, cond });
                state.lastCond = cond;
                state.history.push({ res, cond });
            }

            const run = /^run\s*=\s*(\S+)/i.exec(line);
            if (run) {
                const target = run[1];
                if (
                    !visiting.has(target) &&
                    !RUN_SKIP_PREFIXES.some((prefix) => target.startsWith(prefix))
                ) {
                    const nested = sectionLookup(sections, target);
                    if (nested && runExpansions < MAX_RUN_EXPANSIONS) {
                        runExpansions += 1;
                        visiting.add(target);
                        scan(nested, info, condStack, visiting, target);
                        visiting.delete(target);
                    }
                }
            }
        }
    };

    const secInfo: Record<string, SectionInfo> = {};
    for (const [name, lines] of Object.entries(sections)) {
        if (!name.startsWith("TextureOverride") && !name.startsWith("CommandList")) {
            continue;
        }
        const info: SectionInfo = {
            draws: [],
            diffusePool: [],
            handlingSkip: false,
            auxDrawStates: [],
            diffuseVariantsAtEnd: [],
            diffuseHistoryAtEnd: [],
            auxMapsAtEnd: {},
            curDiffuseVariants: [],
            diffuseHistory: [],
            nonDiffuseSlotPool: [],
            ibHistory: [],
            vb0History: [],
            vb1History: [],
            thisHistory: [],
            auxMaps: {},
        };
        scan(lines, info, [], new Set([name]), name);
        info.diffuseVariantsAtEnd = [...info.curDiffuseVariants];
        info.diffuseHistoryAtEnd = [...info.diffuseHistory];
        info.auxMapsAtEnd = auxSnapshot(info);
        secInfo[name] = info;
    }
    collapseSharedHashTextureOverrides(secInfo, sections);
    attachHashTextureOverrides(secInfo);
    return secInfo;
}

function collectResourceCopySources(
    sections: IniSections,
    resources: Record<string, ResourceInfo>,
): Record<string, string[]> {
    const copySources: Record<string, string[]> = {};
    const copyRe = /^\s*(Resource\S+)\s*=\s*copy(?:\s+ref)?\s+(Resource\S+)\s*$/i;
    for (const lines of Object.values(sections)) {
        for (const raw of lines) {
            const match = copyRe.exec(stripComment(raw.text));
            if (!match || match[1].toLowerCase() === match[2].toLowerCase()) {
                continue;
            }
            const bucket = copySources[match[1].toLowerCase()] ?? [];
            if (bucket.every((existing) => existing.toLowerCase() !== match[2].toLowerCase())) {
                bucket.push(match[2]);
            }
            copySources[match[1].toLowerCase()] = bucket;
        }
    }

    const csReadRe = /^\s*cs-t([12])\s*=\s*(?:ref\s+)?(\S+)\s*$/i;
    const csWriteRe = /^\s*cs-u0\s*=\s*(?:ref\s+)?(\S+)\s*$/i;
    for (const lines of Object.values(sections)) {
        const csInputs: Record<string, string> = {};
        for (const raw of lines) {
            const line = stripComment(raw.text);
            const read = csReadRe.exec(line);
            if (read) {
                if (read[2].toLowerCase() === "null") {
                    delete csInputs[read[1]];
                } else {
                    csInputs[read[1]] = read[2];
                }
                continue;
            }
            const write = csWriteRe.exec(line);
            if (!write || write[1].toLowerCase() === "null") {
                continue;
            }
            const position = csInputs["1"];
            const blend = csInputs["2"];
            if (
                position &&
                blend &&
                resourceLookup(resources, position).filename &&
                resourceLookup(resources, blend).stride === 32
            ) {
                const bucket = copySources[write[1].toLowerCase()] ?? [];
                if (bucket.every((existing) => existing.toLowerCase() !== position.toLowerCase())) {
                    bucket.push(position);
                }
                copySources[write[1].toLowerCase()] = bucket;
            }
        }
    }
    return copySources;
}

function resolveVertexInfo(
    resName: string | undefined,
    resources: Record<string, ResourceInfo>,
    copySources: Record<string, string[]>,
    cache = new Map<string, ResourceInfo>(),
    visiting = new Set<string>(),
): ResourceInfo {
    if (!resName) {
        return {};
    }
    const cacheKey = resName.toLowerCase();
    const cached = cache.get(cacheKey);
    if (cached) {
        return cached;
    }
    const info = resourceLookup(resources, resName);
    if (info.filename) {
        cache.set(cacheKey, info);
        return info;
    }
    if (visiting.has(cacheKey)) {
        return {};
    }
    visiting.add(cacheKey);
    const candidates = [...(copySources[cacheKey] ?? [])];
    if (!cacheKey.endsWith(".b")) {
        candidates.push(`${resName}.B`);
    }
    for (const candidate of candidates) {
        const resolved = resolveVertexInfo(candidate, resources, copySources, cache, visiting);
        if (resolved.filename) {
            cache.set(cacheKey, resolved);
            return resolved;
        }
    }
    cache.set(cacheKey, {});
    return {};
}

function resolveComponentBuffers(
    secInfo: Record<string, SectionInfo>,
    resources: Record<string, ResourceInfo>,
    copySources: Record<string, string[]>,
) {
    const componentPositions: Record<string, string> = {};
    const componentTexcoords: Record<string, string> = {};
    const componentPositionVariants: Record<string, TextureAssignment[]> = {};
    const componentTexcoordVariants: Record<string, TextureAssignment[]> = {};
    const hashPositions: Record<string, string> = {};
    const hashTexcoords: Record<string, string> = {};

    for (const [name, info] of Object.entries(secInfo)) {
        if (!name.startsWith("TextureOverride")) {
            continue;
        }
        const base = name.slice("TextureOverride".length);
        if (base.endsWith("Texcoord") && info.vb1) {
            const component = base.slice(0, -"Texcoord".length);
            componentTexcoords[component] ??= info.vb1;
            componentTexcoordVariants[component] =
                info.vb1History.length > 0 ? info.vb1History : [{ res: info.vb1, cond: DNF_TRUE }];
        }
    }
    for (const [name, info] of Object.entries(secInfo)) {
        if (!name.startsWith("TextureOverride")) {
            continue;
        }
        const base = name.slice("TextureOverride".length);
        if (base.endsWith("Blend")) {
            const component = base.slice(0, -"Blend".length);
            if (info.vb0) {
                componentPositions[component] ??= info.vb0;
            }
            if (
                info.vb1 &&
                !componentTexcoords[component] &&
                resourceLookup(resources, info.vb1).stride !== 32
            ) {
                componentTexcoords[component] = info.vb1;
            }
        } else if (base.endsWith("Position") && info.vb0) {
            const component = base.slice(0, -"Position".length);
            componentPositions[component] ??= info.vb0;
            componentPositionVariants[component] =
                info.vb0History.length > 0 ? info.vb0History : [{ res: info.vb0, cond: DNF_TRUE }];
        }
        const hash = extractHash(name);
        if (hash) {
            if (info.vb0) {
                hashPositions[hash] ??= info.vb0;
            }
            const vb2Stride = info.vb2 ? (resourceLookup(resources, info.vb2).stride ?? 0) : 0;
            const tc = (info.vb2 && vb2Stride !== 32 ? info.vb2 : undefined) ?? info.vb1;
            if (tc) {
                hashTexcoords[hash] ??= tc;
            }
        }
    }

    const componentBuffers: Record<string, { position: string; texcoord: string }> = {};
    for (const [component, position] of Object.entries(componentPositions)) {
        if (componentTexcoords[component]) {
            componentBuffers[component] = { position, texcoord: componentTexcoords[component] };
        }
    }

    let globalIb: string | undefined;
    let globalPosition: string | undefined;
    let globalTexcoord: string | undefined;
    for (const [name, info] of Object.entries(secInfo)) {
        if (!name.startsWith("CommandList")) {
            continue;
        }
        globalIb ??= info.ib;
        globalPosition ??= info.vb0;
        globalTexcoord ??=
            nonBlendVertexRes(info.vb2, resources) ?? nonBlendVertexRes(info.vb1, resources);
    }
    if (globalPosition && !resolveVertexInfo(globalPosition, resources, copySources).filename) {
        for (const [resName, info] of Object.entries(resources)) {
            if (info.filename && (info.format ?? "").includes("R32G32B32")) {
                globalPosition = resName;
                break;
            }
        }
    }

    return {
        componentBuffers,
        componentPositions,
        componentTexcoords,
        componentPositionVariants,
        componentTexcoordVariants,
        hashPositions,
        hashTexcoords,
        globalIb,
        globalPosition,
        globalTexcoord,
    };
}

function selectDrawSections(
    secInfo: Record<string, SectionInfo>,
    globalIb?: string,
): Array<[string, SectionInfo]> {
    return Object.entries(secInfo).filter(
        ([name, info]) =>
            name.startsWith("TextureOverride") &&
            (info.ib || globalIb) &&
            (info.draws.length > 0 || (info.ib && !info.handlingSkip)),
    );
}

function lookupCompValue<T>(mapping: Record<string, T>, component: string): T | undefined {
    if (mapping[component]) {
        return mapping[component];
    }
    const strippedLetters = component.replace(/[A-Za-z]+$/, "");
    if (strippedLetters && strippedLetters !== component && mapping[strippedLetters]) {
        return mapping[strippedLetters];
    }
    const strippedWord = component.replace(/(?<=.)[A-Z][a-z]+$/, "");
    if (strippedWord && strippedWord !== component) {
        return mapping[strippedWord];
    }
    return undefined;
}

function collapseSharedHashTextureOverrides(
    secInfo: Record<string, SectionInfo>,
    sections: IniSections,
): void {
    const grouped = Object.keys(secInfo).reduce((map, name) => {
        const role = name.startsWith("TextureOverride")
            ? HASH_TEXTURE_SUFFIX.exec(name)?.[1]
            : undefined;
        const meta = role ? sectionMatchMeta(sections[name] ?? []) : undefined;
        if (!role || !meta?.hash) {
            return map;
        }
        const key = `${role.toLowerCase()}:${meta.hash}:${meta.matchFirstIndex ?? ""}`;
        return map.set(key, [...(map.get(key) ?? []), { name, role, priority: meta.priority }]);
    }, new Map<string, Array<{ name: string; role: string; priority: number }>>());

    for (const group of grouped.values()) {
        if (group.length < 2) {
            continue;
        }
        const winner = group.reduce((best, entry) =>
            entry.priority >= best.priority ? entry : best,
        );
        const source = secInfo[winner.name];
        for (const entry of group) {
            if (entry.name !== winner.name) {
                replaceHashTextureSection(secInfo[entry.name], source, winner.role);
            }
        }
    }
}

function sectionMatchMeta(lines: IniLine[]): {
    hash?: string;
    matchFirstIndex?: string;
    priority: number;
} {
    return lines.reduce<{ hash?: string; matchFirstIndex?: string; priority: number }>(
        (meta, raw) => {
            const line = stripComment(raw.text);
            const hash = /^hash\s*=\s*([0-9a-f]+)/i.exec(line)?.[1];
            const matchFirstIndex = /^match_first_index\s*=\s*(\S+)/i.exec(line)?.[1];
            const priority = /^match_priority\s*=\s*(-?\d+)/i.exec(line)?.[1];
            return {
                hash: hash ? hash.toLowerCase() : meta.hash,
                matchFirstIndex: matchFirstIndex ?? meta.matchFirstIndex,
                priority: priority !== undefined ? Number(priority) : meta.priority,
            };
        },
        { priority: 0 },
    );
}

function replaceHashTextureSection(target: SectionInfo, source: SectionInfo, role: string): void {
    if (/^Diffuse$/i.test(role)) {
        if (!source.diffuse) {
            return;
        }
        target.diffuse = source.diffuse;
        target.diffusePool = [...source.diffusePool];
        target.diffuseVariantsAtEnd = [...source.diffuseVariantsAtEnd];
        target.diffuseHistoryAtEnd = [...source.diffuseHistoryAtEnd];
        target.diffuseHistory = [...source.diffuseHistory];
        target.curDiffuseVariants = [...source.curDiffuseVariants];
        for (const draw of target.draws) {
            draw.diffuseVariants = [...source.diffuseHistory];
            draw.diffuseHistory = [...source.diffuseHistory];
        }
        return;
    }
    const channel = AUX_MAP_CHANNELS[role.toLowerCase()];
    if (!channel) {
        return;
    }
    const state = source.auxMapsAtEnd[channel] ?? source.auxMaps[channel];
    if (!state || (state.history.length === 0 && state.variants.length === 0)) {
        return;
    }
    const copied = {
        variants: [...state.variants],
        history: [...(state.history.length > 0 ? state.history : state.variants)],
    };
    target.auxMaps[channel] = {
        ...copied,
        chainKey: null,
        lastCond: null,
    };
    target.auxMapsAtEnd[channel] = copied;
    for (const drawState of target.auxDrawStates) {
        drawState[channel] = {
            variants: [...copied.variants],
            history: [...copied.history],
        };
    }
}

function attachHashTextureOverrides(secInfo: Record<string, SectionInfo>): void {
    for (const [name, info] of Object.entries(secInfo)) {
        if (!name.startsWith("TextureOverride") || HASH_TEXTURE_SUFFIX.test(name)) {
            continue;
        }
        copyDiffuseIfMissing(info, lookupFamilyTextureSection(secInfo, name, "Diffuse"));
        copyAuxIfMissing(
            info,
            lookupFamilyTextureSection(secInfo, name, "NormalMap"),
            "normal_map",
        );
        copyAuxIfMissing(info, lookupFamilyTextureSection(secInfo, name, "LightMap"), "light_map");
        copyAuxIfMissing(
            info,
            lookupFamilyTextureSection(secInfo, name, "MaterialMap"),
            "material_map",
        );
    }
}

function lookupFamilyTextureSection(
    secInfo: Record<string, SectionInfo>,
    name: string,
    suffix: string,
): SectionInfo | undefined {
    const exact = lookupSectionInfo(secInfo, `${name}${suffix}`);
    if (exact) {
        return exact;
    }
    if (!/[A-Z]$/.test(name)) {
        return undefined;
    }
    const family = name.slice(0, -1);
    return (
        lookupSectionInfo(secInfo, `${family}A${suffix}`) ??
        lookupSectionInfo(secInfo, `${family}${suffix}`)
    );
}

function lookupSectionInfo(
    secInfo: Record<string, SectionInfo>,
    name: string,
): SectionInfo | undefined {
    if (secInfo[name]) {
        return secInfo[name];
    }
    const lowered = name.toLowerCase();
    return Object.entries(secInfo).find(([key]) => key.toLowerCase() === lowered)?.[1];
}

function copyDiffuseIfMissing(target: SectionInfo, source?: SectionInfo): void {
    if (!source?.diffuse || target.diffuse) {
        return;
    }
    target.diffuse = source.diffuse;
    for (const res of source.diffusePool) {
        if (!target.diffusePool.includes(res)) {
            target.diffusePool.push(res);
        }
    }
    target.diffuseVariantsAtEnd = [...source.diffuseVariantsAtEnd];
    target.diffuseHistoryAtEnd = [...source.diffuseHistoryAtEnd];
    target.diffuseHistory = [...source.diffuseHistory];
    target.curDiffuseVariants = [...source.curDiffuseVariants];
    for (const draw of target.draws) {
        if (draw.diffuseVariants.length === 0) {
            draw.diffuseVariants = [...source.diffuseHistory];
        }
        if (draw.diffuseHistory.length === 0) {
            draw.diffuseHistory = [...source.diffuseHistory];
        }
    }
}

function copyAuxIfMissing(
    target: SectionInfo,
    source: SectionInfo | undefined,
    channel: string,
): void {
    if (
        !source ||
        target.auxMaps[channel]?.history.length ||
        target.auxMapsAtEnd[channel]?.history.length
    ) {
        return;
    }
    const state = source.auxMapsAtEnd[channel] ?? source.auxMaps[channel];
    if (!state || (state.history.length === 0 && state.variants.length === 0)) {
        return;
    }
    const copied = {
        variants: [...state.variants],
        history: [...(state.history.length > 0 ? state.history : state.variants)],
    };
    target.auxMaps[channel] = {
        ...copied,
        chainKey: null,
        lastCond: null,
    };
    target.auxMapsAtEnd[channel] = copied;
    const clone = () => ({
        variants: [...copied.variants],
        history: [...copied.history],
    });
    if (target.auxDrawStates.length === 0 && target.draws.length > 0) {
        target.auxDrawStates = target.draws.map(() => ({ [channel]: clone() }));
        return;
    }
    for (const state of target.auxDrawStates) {
        if (!state[channel]?.history.length && !state[channel]?.variants.length) {
            state[channel] = clone();
        }
    }
}

function lookupRoleResource(
    ibRes: string | undefined,
    role: string,
    resources: Record<string, ResourceInfo>,
): string | undefined {
    if (!ibRes) {
        return undefined;
    }
    const stem = ibRes.replace(/\.\d+$/, "").replace(/IB$/i, "");
    const family = stem.replace(/[A-Z]$/, "");
    const candidates = [`${stem}${role}`];
    if (family !== stem) {
        candidates.push(`${family}A${role}`, `${family}${role}`);
    }
    for (const candidate of candidates) {
        const want = candidate.toLowerCase();
        const found = Object.keys(resources).find((name) => {
            const lowered = name.toLowerCase();
            return lowered === want || lowered.startsWith(`${want}.`);
        });
        if (found) {
            return found;
        }
    }
    return undefined;
}

function ibResToComponent(ibRes: string): string {
    let value = ibRes.startsWith("Resource") ? ibRes.slice(8) : ibRes;
    value = value.replace(/\.\d+$/, "");
    if (value.endsWith("IB")) {
        value = value.slice(0, -2);
    }
    return value.replace(/[A-Z]$/, "");
}

function resolveDrawNumber(token: string, constants: Record<string, string>): number | null {
    const trimmed = token.trim();
    if (/^-?\d+$/.test(trimmed)) {
        const value = Number(trimmed);
        return value >= 0 ? value : null;
    }
    const match = /^\$(\w+)$/.exec(trimmed);
    if (!match) {
        return null;
    }
    const raw =
        constants[match[1]] ??
        Object.entries(constants).find(
            ([key]) => key.toLowerCase() === match[1].toLowerCase(),
        )?.[1];
    if (raw === undefined || !/^-?\d+(?:\.\d+)?$/.test(raw.trim())) {
        return null;
    }
    const value = Number(raw.trim());
    return Number.isFinite(value) && value >= 0 ? value : null;
}

function toggleValueDomainsFromKeys(
    toggleKeys: ReturnType<typeof extractToggleKeys>,
): Record<string, string[]> {
    const domains: Record<string, string[]> = {};
    for (const info of Object.values(toggleKeys)) {
        for (const [variable, values] of Object.entries(info.vars)) {
            domains[variable] = values;
            domains[variable.toLowerCase()] = values;
        }
    }
    return domains;
}

function implicitDrawsFromIbHistory(info: SectionInfo): {
    draws: DrawSnapshot[];
    auxDrawStates: SectionInfo["auxDrawStates"];
} {
    const constrained = lastAssignmentByCond(
        info.ibHistory.filter(
            (entry) => !isUnconstrained(entry.cond) && !sameDnf(entry.cond, DNF_FALSE),
        ),
    );
    if (constrained.length === 0) {
        return {
            draws: [
                {
                    count: null,
                    start: 0,
                    base: 0,
                    conditions: DNF_TRUE,
                    source: info.src,
                    ib: undefined,
                    diffuseVariants: info.diffuseVariantsAtEnd,
                    diffuseHistory: info.diffuseHistoryAtEnd,
                    vb: [undefined, undefined, undefined],
                },
            ],
            auxDrawStates: [],
        };
    }
    return {
        draws: constrained.map((assignment) => ({
            count: null,
            start: 0,
            base: 0,
            conditions: assignment.cond,
            source: info.src,
            ib: assignment.res,
            diffuseVariants: info.diffuseHistory.filter((entry) =>
                dnfCovers(assignment.cond, entry.cond),
            ),
            diffuseHistory: info.diffuseHistory.filter((entry) =>
                dnfCovers(assignment.cond, entry.cond),
            ),
            vb: [undefined, undefined, undefined],
        })),
        auxDrawStates: constrained.map((assignment) =>
            filterAux(info.auxMapsAtEnd, assignment.cond),
        ),
    };
}

function lastAssignmentByCond(assignments: TextureAssignment[]): TextureAssignment[] {
    const last = new Map<string, TextureAssignment>();
    const order: string[] = [];
    for (const assignment of assignments) {
        const key = jsonDnf(assignment.cond);
        if (!last.has(key)) {
            order.push(key);
        }
        last.set(key, assignment);
    }
    return order.map((key) => last.get(key)!);
}

function filterAux(
    auxMaps: Record<string, { variants: TextureAssignment[]; history: TextureAssignment[] }>,
    cond: Dnf,
): Record<string, { variants: TextureAssignment[]; history: TextureAssignment[] }> {
    const snapshot: Record<
        string,
        { variants: TextureAssignment[]; history: TextureAssignment[] }
    > = {};
    for (const [channel, state] of Object.entries(auxMaps)) {
        const history = state.history.filter((entry) => dnfCovers(cond, entry.cond));
        if (history.length > 0) {
            snapshot[channel] = { variants: history, history };
        }
    }
    return snapshot;
}

function pickVariant(assignments: TextureAssignment[] | undefined, cond: Dnf): string | undefined {
    if (!assignments?.length) {
        return undefined;
    }
    const matched = [...assignments]
        .reverse()
        .find((assignment) => dnfCovers(cond, assignment.cond));
    return matched?.res ?? assignments[0].res;
}

function collapseAnimationDraws(groups: DrawGroup[], animationVars?: Set<string>): DrawGroup[] {
    if (!animationVars?.size) {
        return groups;
    }
    return groups.map((group) => {
        const buckets = new Map<string, DrawRecord[]>();
        const order: string[] = [];
        for (const draw of group.draws) {
            const key = animationMergeKey(draw, animationVars);
            if (!buckets.has(key)) {
                buckets.set(key, []);
                order.push(key);
            }
            buckets.get(key)!.push(draw);
        }
        return {
            ...group,
            draws: order.map((key) => mergeAnimationDraws(buckets.get(key)!, group, animationVars)),
        };
    });
}

function animationMergeKey(draw: DrawRecord, animationVars: Set<string>): string {
    return [
        draw.ibFile ?? "",
        draw.start,
        draw.count,
        draw.base,
        draw.texcoordFile ?? "",
        draw.textureDefaultFile ?? "",
        jsonDnf(stripAnimationVars(draw.conditions, animationVars)),
    ].join("|");
}

function mergeAnimationDraws(
    draws: DrawRecord[],
    group: DrawGroup,
    animationVars: Set<string>,
): DrawRecord {
    const first = draws[0];
    if (draws.length === 1) {
        return first;
    }
    const variants = draws.flatMap((draw) => {
        if (draw.positionVariants?.length) {
            return draw.positionVariants;
        }
        return [
            {
                conditions: draw.conditions,
                file: draw.positionFile ?? group.positionFile,
                stride: draw.positionStride ?? group.positionStride,
            },
        ];
    });
    const unique: NonNullable<DrawRecord["positionVariants"]> = [];
    for (const variant of variants) {
        if (
            unique.some(
                (existing) =>
                    existing.file === variant.file &&
                    jsonDnf(existing.conditions) === jsonDnf(variant.conditions),
            )
        ) {
            continue;
        }
        unique.push(variant);
    }
    return {
        ...first,
        conditions: stripAnimationVars(first.conditions, animationVars),
        positionFile: first.positionFile,
        positionVariants: unique.length > 1 ? unique : first.positionVariants,
    };
}

function stripAnimationVars(dnf: Dnf, animationVars: Set<string>): Dnf {
    if (dnf.length === 0) {
        return DNF_FALSE;
    }
    const tracked = new Set([...animationVars].map((variable) => variable.toLowerCase()));
    const out: Dnf = [];
    for (const group of dnf) {
        const kept = group.filter((clause) => !tracked.has(clause.var.toLowerCase()));
        if (kept.length === 0) {
            return DNF_TRUE;
        }
        if (!out.some((existing) => jsonDnf([existing]) === jsonDnf([kept]))) {
            out.push(kept);
        }
    }
    return out.length > 0 ? out : DNF_FALSE;
}

function nonBlendVertexRes(
    res: string | undefined,
    resources: Record<string, ResourceInfo>,
): string | undefined {
    if (!res || resourceLookup(resources, res).stride === 32) {
        return undefined;
    }
    return res;
}

function ibIndexSize(format?: string): number {
    return (format ?? "").toUpperCase().includes("R16") ? 2 : 4;
}

function extractHash(name: string): string | undefined {
    return (
        /_([0-9a-f]{8})_/i.exec(name)?.[1].toLowerCase() ??
        /[0-9a-f]{8}/i.exec(name)?.[0].toLowerCase()
    );
}

function auxSnapshot(info: SectionInfo) {
    const snapshot: Record<
        string,
        { variants: TextureAssignment[]; history: TextureAssignment[] }
    > = {};
    for (const [channel, state] of Object.entries(info.auxMaps)) {
        if (state.variants.length > 0 || state.history.length > 0) {
            snapshot[channel] = {
                variants: [...state.variants],
                history: [...state.history],
            };
        }
    }
    return snapshot;
}

function jsonDnf(dnf: Dnf): string {
    return JSON.stringify(dnf);
}
