import { open } from "node:fs/promises";
import path from "node:path";

import type { Dnf } from "@shared/mod-viewer/types";
import fse from "fs-extra";

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
const AUX_MAP_CHANNELS: Record<string, "normal_map" | "light_map" | "material_map"> = {
    normalmap: "normal_map",
    lightmap: "light_map",
    materialmap: "material_map",
};
const HASH_TEXTURE_SUFFIX = /(Diffuse|NormalMap|LightMap|MaterialMap)$/i;
const WWMI_DUMP_TEX_RE = /(?:^|[/\\])Components-(\d+)\s+t=/i;
const DDS_SRGB_DXGI = new Set([29, 72, 75, 78, 91, 93, 99]);

export type TextureAssignment = { res: string; cond: Dnf };

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
    diffuseHistory: TextureAssignment[];
    ibHistory: TextureAssignment[];
    vb0History: TextureAssignment[];
    vb1History: TextureAssignment[];
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
            } else {
                const fallback = lookupRoleResource(snapshot.ib ?? ibRes, "Diffuse", resources);
                const file = fallback ? resourceLookup(resources, fallback).filename : undefined;
                if (file) {
                    draw.textureDefaultFile = file;
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
            indexSize: ibIndexSize(ibRi.format),
            draws,
        });
    }
    return collapseAnimationDraws(groups, animation?.vars);
}

export async function attachWwmiDumpTextures(
    groups: DrawGroup[],
    resources: Record<string, ResourceInfo>,
    modDir: string,
): Promise<void> {
    const needed = new Set(
        groups.flatMap((group) => {
            const index = wwmiComponentIndex(group);
            if (!index || group.draws.every((draw) => draw.textureDefaultFile)) {
                return [];
            }
            return [index];
        }),
    );
    if (needed.size === 0) {
        return;
    }

    const filesByIndex = Object.values(resources).reduce((map, info) => {
        const file = info.filename;
        const index = file ? WWMI_DUMP_TEX_RE.exec(file.replaceAll("\\", "/"))?.[1] : undefined;
        if (!file || !index || !needed.has(index)) {
            return map;
        }
        return map.set(index, [...(map.get(index) ?? []), file]);
    }, new Map<string, string[]>());

    const pickedByIndex = new Map(
        (
            await Promise.all(
                [...filesByIndex].map(async ([index, files]) => {
                    const scored = (
                        await Promise.all(
                            files.map(async (file, order) => {
                                const resolved = safeResourcePath(modDir, file);
                                if (!resolved || !(await fse.pathExists(resolved))) {
                                    return null;
                                }
                                const hint = await inspectWwmiTextureHint(resolved).catch(
                                    () => null,
                                );
                                if (!hint) {
                                    return null;
                                }
                                return {
                                    file,
                                    order,
                                    ...hint,
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

    for (const group of groups) {
        const picked = pickedByIndex.get(wwmiComponentIndex(group) ?? "");
        if (!picked) {
            continue;
        }
        group.diffuseFile ??= picked;
        for (const draw of group.draws) {
            draw.textureDefaultFile ??= picked;
        }
    }
}

export function pickWwmiDumpDiffuse(
    candidates: Array<{ file: string; srgb: boolean; area: number; bytes: number; order: number }>,
): string | undefined {
    const ranked = [...candidates].sort(
        (left, right) =>
            Number(right.srgb) - Number(left.srgb) ||
            right.area - left.area ||
            right.bytes - left.bytes ||
            left.order - right.order,
    );
    return ranked[0]?.file;
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

async function inspectWwmiTextureHint(
    filePath: string,
): Promise<{ srgb: boolean; area: number; bytes: number }> {
    const bytes = (await fse.stat(filePath)).size;
    const ext = path.extname(filePath).toLowerCase();
    if (ext === ".png") {
        return { srgb: true, area: await readPngArea(filePath), bytes };
    }
    if (ext === ".jpg" || ext === ".jpeg") {
        return { srgb: true, area: 0, bytes };
    }
    if (ext !== ".dds" || bytes < 128) {
        return { srgb: false, area: 0, bytes };
    }
    const header = await readFilePrefix(filePath, Math.min(148, bytes));
    if (header.length < 128) {
        return { srgb: false, area: 0, bytes };
    }
    const area = header.readUInt32LE(16) * header.readUInt32LE(12);
    if (header.toString("ascii", 84, 88) !== "DX10" || header.length < 132) {
        return { srgb: false, area, bytes };
    }
    return { srgb: DDS_SRGB_DXGI.has(header.readUInt32LE(128)), area, bytes };
}

async function readPngArea(filePath: string): Promise<number> {
    const header = await readFilePrefix(filePath, 24);
    if (header.length < 24) {
        return 0;
    }
    return header.readUInt32BE(16) * header.readUInt32BE(20);
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
                info.vb0 ??= value;
                info.curVb0 = value;
                if (value) {
                    info.vb0History.push({ res: value, cond: stackedCond(condStack) });
                }
            }
            const vb1 = /^vb1\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
            if (vb1) {
                const value = vb1[1].toLowerCase() === "null" ? undefined : vb1[1];
                info.vb1 ??= value;
                info.curVb1 = value;
                if (value) {
                    info.vb1History.push({ res: value, cond: stackedCond(condStack) });
                }
            }
            const vb2 = /^vb2\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
            if (vb2) {
                const value = vb2[1].toLowerCase() === "null" ? undefined : vb2[1];
                info.vb2 ??= value;
                info.curVb2 = value;
            }
            const ib = /^ib\s*=\s*(\S+)/i.exec(line);
            if (ib) {
                info.ib ??= ib[1];
                info.curIb = ib[1];
                info.ibHistory.push({ res: ib[1], cond: stackedCond(condStack) });
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

            let diffuse = /^Resource\\[^\\]+\\Diffuse\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
            if (!diffuse) {
                const ps = /^ps-t\d+\s*=\s*(\S+)/i.exec(line);
                if (ps && /Diffuse/i.test(ps[1])) {
                    diffuse = ps;
                }
            }
            if (!diffuse) {
                const self = /^this\s*=\s*(?:ref\s+)?(\S+)/i.exec(line);
                if (self && (/Diffuse/i.test(self[1]) || /^Diffuse$/i.test(sectionRole ?? ""))) {
                    diffuse = self;
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
                info.curDiffuseVariants.push({ res, cond });
                info.diffuseLastCond = cond;
                info.diffuseHistory.push({ res, cond });
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
                    if (nested) {
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
            ibHistory: [],
            vb0History: [],
            vb1History: [],
            auxMaps: {},
        };
        scan(lines, info, [], new Set([name]), name);
        info.diffuseVariantsAtEnd = [...info.curDiffuseVariants];
        info.diffuseHistoryAtEnd = [...info.diffuseHistory];
        info.auxMapsAtEnd = auxSnapshot(info);
        secInfo[name] = info;
    }
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
        return Number(trimmed);
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
    return Number.isFinite(value) ? value : null;
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
        info.ibHistory.filter((entry) => !isUnconstrained(entry.cond)),
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
