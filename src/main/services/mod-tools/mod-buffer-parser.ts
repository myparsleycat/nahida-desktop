import path from "node:path";

import type { IniSection, Resource } from "@main/lib/mod-static-glb/types";
import fse from "fs-extra";

export type CommandListMap = Map<string, IniSection>;

export function collectResources(sections: IniSection[]): Resource[] {
    return sections
        .filter((section) => section.header === "Resource")
        .map((section) => ({
            name: section.name,
            filename: sectionValueFromLines(section.lines, "filename"),
            stride: Number(sectionValueFromLines(section.lines, "stride")) || undefined,
            format: sectionValueFromLines(section.lines, "format"),
            values: section.values,
        }));
}

export function isLodResourceName(name: string): boolean {
    return /_LOD$/i.test(name) || /_VB\d+_LOD/i.test(name);
}

export function collectPositionResources(resources: Resource[]): Resource[] {
    return resources.filter((resource) => {
        if (!resource.filename || !resource.stride) return false;
        if (isLodResourceName(resource.name) || /position(?:\.\d+)?cs$/i.test(resource.name)) {
            return false;
        }
        if (/position/i.test(resource.name) && resource.stride >= 12) return true;
        // Native EFMI: ComponentN_VB0 is POSITION (xyz float32 + pad).
        if (/component\d+_vb0$/i.test(resource.name) && resource.stride >= 12) return true;
        return false;
    });
}

export function collectIndexResources(resources: Resource[]): Resource[] {
    return resources.filter((resource) => {
        if (!resource.filename) return false;
        if (isLodResourceName(resource.name)) return false;
        if (/index/i.test(resource.name)) return true;
        // EFMI ComponentN IBs use R16/R32_UINT without "Index" in the name.
        // Exclude blend/color-style byte formats (R8_UINT) and non-index names that
        // already identify as position/blend/vector/texcoord/color.
        if (
            resource.format &&
            /R(?:16|32)_UINT/i.test(resource.format) &&
            !/(position|blend|vector|texcoord|color)/i.test(resource.name)
        ) {
            return true;
        }
        return false;
    });
}

export function collectCommandLists(sections: IniSection[]): CommandListMap {
    return new Map(
        sections
            .filter((section) => section.header === "CommandList")
            .map((section) => [`commandlist${section.name}`.toLowerCase(), section] as const),
    );
}

export function expandCommandListLines(
    lines: string[],
    commandLists: CommandListMap,
    stack = new Set<string>(),
): string[] {
    return lines.flatMap((line) => {
        const reference = line.trim().match(/^run\s*=\s*(CommandList[^\s;]+)(?:\s*;.*)?$/i)?.[1];
        if (!reference) return [line];

        const key = reference.toLowerCase();
        const commandList = commandLists.get(key);
        if (!commandList || stack.has(key)) return [line];

        return expandCommandListLines(commandList.lines, commandLists, new Set([...stack, key]));
    });
}

export function sectionValueFromLines(lines: string[], key: string): string | undefined {
    const normalizedKey = key.toLowerCase();
    const line = lines.findLast((entry) => {
        const separator = entry.indexOf("=");
        return separator >= 0 && entry.slice(0, separator).trim().toLowerCase() === normalizedKey;
    });
    if (!line) return undefined;
    return line.slice(line.indexOf("=") + 1).trim();
}

export function matchIndexResources(
    positions: Resource[],
    indices: Resource[],
    sections: IniSection[],
    commandLists = collectCommandLists(sections),
): Map<string, Resource[]> {
    const matches = new Map<string, Resource[]>();
    const resourcesByName = new Map(
        [...positions, ...indices].map((resource) => [resourceKey(resource), resource]),
    );

    for (const section of sections) {
        const lines = expandCommandListLines(section.lines, commandLists);
        const position = resourceForReference(sectionValueFromLines(lines, "vb0"), resourcesByName);
        const index = resourceForReference(sectionValueFromLines(lines, "ib"), resourcesByName);
        if (position && index) addIndexMatch(matches, position, index);
    }

    for (const index of indices) {
        const candidates = positions
            .map((position) => ({ position, score: indexMatchScore(position, index) }))
            .filter((candidate) => candidate.score > 0);
        const bestScore = Math.max(...candidates.map((candidate) => candidate.score), 0);
        const best = candidates.filter((candidate) => candidate.score === bestScore);
        if (best.length === 1) addIndexMatch(matches, best[0].position, index);
    }

    if (indices.length === 1) {
        for (const position of positions) {
            if (!matches.has(resourceKey(position))) addIndexMatch(matches, position, indices[0]);
        }
    }

    for (const matched of matches.values()) {
        matched.sort((left, right) => indices.indexOf(left) - indices.indexOf(right));
    }
    return matches;
}

export function resourceForReference(
    value: string | undefined,
    resourcesByName: Map<string, Resource>,
): Resource | undefined {
    const name = value?.trim().match(/^(?:ref\s+)?Resource(.+)$/i)?.[1];
    return name ? resourcesByName.get(name.toLowerCase()) : undefined;
}

export function resourceKey(resource: Resource): string {
    return resource.name.toLowerCase();
}

export async function readIndexBuffer(filePath: string, format?: string): Promise<Uint32Array> {
    const bytes = await fse.readFile(filePath);
    const use16 =
        format?.toUpperCase().includes("R16") ||
        (bytes.byteLength % 4 !== 0 && bytes.byteLength % 2 === 0);

    if (use16) {
        const src = new Uint16Array(
            bytes.buffer,
            bytes.byteOffset,
            Math.floor(bytes.byteLength / 2),
        );
        return Uint32Array.from(src);
    }

    return new Uint32Array(bytes.buffer, bytes.byteOffset, Math.floor(bytes.byteLength / 4));
}

export function combineIndexBuffers(buffers: Uint32Array[]): Uint32Array {
    if (buffers.length === 0) return new Uint32Array();
    if (buffers.length === 1) return buffers[0];

    const indices = new Uint32Array(buffers.reduce((length, buffer) => length + buffer.length, 0));
    buffers.reduce((offset, buffer) => {
        indices.set(buffer, offset);
        return offset + buffer.length;
    }, 0);
    return indices;
}

function addIndexMatch(matches: Map<string, Resource[]>, position: Resource, index: Resource) {
    const key = resourceKey(position);
    const matched = matches.get(key) ?? [];
    if (!matched.some((candidate) => resourceKey(candidate) === resourceKey(index))) {
        matched.push(index);
        matches.set(key, matched);
    }
}

function indexMatchScore(position: Resource, index: Resource) {
    const positionName = logicalResourceName(position.name, "position");
    const indexName = logicalResourceName(index.name, "index");
    if (!positionName.base || !indexName.base) return 0;
    if (positionName.variant && indexName.variant && positionName.variant !== indexName.variant) {
        return 0;
    }
    if (!indexName.base.startsWith(positionName.base)) return 0;
    const variantScore = positionName.variant === indexName.variant ? 100 : 0;
    const exactScore = indexName.base === positionName.base ? 10_000 : 1_000;
    return exactScore + positionName.base.length + variantScore;
}

export function collectVectorResources(resources: Resource[]): Resource[] {
    return resources.filter((resource) => {
        if (!resource.filename) return false;
        return /vector/i.test(resource.name);
    });
}

export function collectBlendResources(resources: Resource[]): Resource[] {
    return resources.filter((resource) => {
        if (!resource.filename) return false;
        if (isLodResourceName(resource.name)) return false;
        if (/blend/i.test(resource.name)) return true;
        if (/component\d+_vb2$/i.test(resource.name)) return true;
        return false;
    });
}

export function matchCompanionResource(
    position: Resource,
    candidates: Resource[],
): Resource | undefined {
    if (candidates.length === 0) return undefined;

    const positionNameKey = companionKey(position.name);
    const exact = candidates.find((candidate) => companionKey(candidate.name) === positionNameKey);
    if (exact) return exact;

    const positionGroup = resourceGroupKey(position);
    if (positionGroup) {
        const byGroup = candidates.find(
            (candidate) => resourceGroupKey(candidate) === positionGroup,
        );
        if (byGroup) return byGroup;
    }

    if (candidates.length === 1) return candidates[0];
    return undefined;
}

function companionKey(name: string): string {
    let key = name
        .replace(/_VB\d+(?:_LOD)?$/i, "")
        .replace(/_IB(?:_LOD)?$/i, "")
        .replace(/(Position|Vector|Index|Blend|TexCoord|Color)Buffer/gi, "")
        .replace(/(Position|Vector|Index|Blend|Texcoord)/gi, "");

    if (!/^_?Component\d+$/i.test(key)) {
        key = key.replace(/[_-]Component\d+$/i, "");
    }

    return key.replace(/[_-]+/g, "").toLowerCase();
}

function resourceGroupKey(resource: Resource): string | undefined {
    if (resource.filename) {
        const stem = path.basename(resource.filename, path.extname(resource.filename));
        const fromStem = companionKey(stem);
        if (fromStem) return fromStem;
    }
    const fromName = companionKey(resource.name);
    return fromName || undefined;
}

function logicalResourceName(name: string, kind: "position" | "index") {
    const withoutCs = name.replace(/cs$/i, "");
    const match = withoutCs.match(
        kind === "position"
            ? /^(.*?)(?:position(?:buffer)?|_vb0)(?:[._-](.+))?$/i
            : /^(.*?)(?:index(?:buffer)?|_ib|ib)(?:[._-](.+))?$/i,
    );
    const withoutKind = match?.[1] ?? withoutCs;
    const withoutLodPrefix =
        kind === "index" ? withoutKind.replace(/^_?lod\d+(?:[._-]?)/i, "") : withoutKind;
    return {
        base: withoutLodPrefix.replace(/[^a-z0-9]/gi, "").toLowerCase(),
        variant: match?.[2]?.replace(/[^a-z0-9]/gi, "").toLowerCase(),
    };
}
