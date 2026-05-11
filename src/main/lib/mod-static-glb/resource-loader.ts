import path from "node:path";
import { interleaveVertexBuffers } from "@native/static-glb";
import fse from "fs-extra";
import { normalizeKey } from "./shared";
import type {
    BufferGroup,
    EfmiBufferResourceGroup,
    IniSection,
    MihoyoBufferResourceGroup,
    Resource,
    StaticGlbModLayout,
    WwmiBufferResourceGroup,
} from "./types";

export function collectResources(sections: IniSection[]): Resource[] {
    return sections
        .filter((section) => section.header === "Resource")
        .map((section) => ({
            name: section.name,
            filename: section.values.filename,
            stride: section.values.stride ? Number(section.values.stride) : undefined,
            format: section.values.format,
            values: section.values,
        }));
}

export function detectStaticGlbModLayout(
    sections: IniSection[],
    resources: Resource[],
): StaticGlbModLayout {
    if (
        sections.some(
            (section) =>
                section.header === "Constants" &&
                section.lines.some((line) => /\$required_efmi_version\b/i.test(line)),
        )
    ) {
        return "efmi";
    }

    if (
        sections.some(
            (section) =>
                section.header === "Constants" &&
                section.lines.some((line) => /\$required_wwmi_version\b/i.test(line)),
        )
    ) {
        return "wwmi";
    }

    const resourceNames = new Set(resources.map((resource) => normalizeKey(resource.name)));
    if (
        resourceNames.has(normalizeKey("IndexBuffer")) &&
        resourceNames.has(normalizeKey("PositionBuffer")) &&
        resourceNames.has(normalizeKey("VectorBuffer")) &&
        resourceNames.has(normalizeKey("TexCoordBuffer"))
    ) {
        return "wwmi";
    }

    return "mihoyo";
}

export async function collectMihoyoBufferGroups(
    modDir: string,
    resources: Resource[],
    warn: (message: string) => void,
): Promise<BufferGroup[]> {
    const byKey = new Map<string, MihoyoBufferResourceGroup>();

    for (const resource of resources) {
        if (!resource.filename || !resource.stride) continue;
        const typedResource = parseMihoyoBufferGroupResourceName(resource.name);
        if (typedResource) {
            const { key, kind } = typedResource;
            if (kind === "position") {
                ensureMihoyoBufferResourceGroup(byKey, key).position = resource;
            } else if (kind === "blend") {
                ensureMihoyoBufferResourceGroup(byKey, key).blend = resource;
            } else {
                ensureMihoyoBufferResourceGroup(byKey, key).texcoord = resource;
            }
        } else if (
            !isShapeKeyPositionVariantResource(resource.name) &&
            (resource.filename.toLowerCase().endsWith(".buf") ||
                resource.filename.toLowerCase().endsWith(".vb"))
        ) {
            ensureMihoyoBufferResourceGroup(byKey, resource.name).single = resource;
        }
    }

    const groups: BufferGroup[] = [];
    for (const [key, group] of byKey) {
        if (group.single?.filename && group.single.stride) {
            const filePath = path.resolve(modDir, group.single.filename);
            if (!(await fse.pathExists(filePath))) {
                warn(`Missing vertex buffer file: ${filePath}`);
                continue;
            }
            groups.push({
                key,
                vbFilename: group.single.filename,
                vbBytes: await fse.readFile(filePath),
                stride: group.single.stride,
            });
            continue;
        }

        if (group.position?.filename && group.blend?.filename && group.texcoord?.filename) {
            const positionResource = group.position;
            const blendResource = group.blend;
            const texcoordResource = group.texcoord;
            const [position, blend, texcoord] = await Promise.all([
                readResourceBytes(modDir, positionResource),
                readResourceBytes(modDir, blendResource),
                readResourceBytes(modDir, texcoordResource),
            ]);
            const positionStride = positionResource.stride;
            const blendStride = blendResource.stride;
            const texcoordStride = texcoordResource.stride;
            if (
                positionStride === undefined ||
                blendStride === undefined ||
                texcoordStride === undefined
            ) {
                warn(`Skipping incomplete interleaved buffer group: ${key}`);
                continue;
            }
            const stride = positionStride + blendStride + texcoordStride;
            const vertexCount = Math.min(
                Math.floor(position.length / positionStride),
                Math.floor(blend.length / blendStride),
                Math.floor(texcoord.length / texcoordStride),
            );
            const vb = interleaveVertexBuffers(
                position,
                positionStride,
                blend,
                blendStride,
                texcoord,
                texcoordStride,
            );
            if (vb.length !== vertexCount * stride) {
                throw new Error(`Unexpected interleaved buffer length for ${key}`);
            }
            groups.push({ key, vbFilename: `${key}.vb`, vbBytes: vb, stride });
        }
    }

    return groups;
}

export async function collectWwmiBufferGroups(
    modDir: string,
    resources: Resource[],
    warn: (message: string) => void,
): Promise<BufferGroup[]> {
    const byKey = new Map<string, WwmiBufferResourceGroup>();

    for (const resource of resources) {
        if (!resource.filename || !resource.stride) continue;
        const typedResource = parseWwmiBufferResourceName(resource.name);
        if (!typedResource) {
            continue;
        }

        const group = ensureWwmiBufferResourceGroup(byKey, typedResource.key);
        switch (typedResource.kind) {
            case "position":
                group.position = resource;
                break;
            case "vector":
                group.vector = resource;
                break;
            case "blend":
                group.blend = resource;
                break;
            case "color":
                group.color = resource;
                break;
            case "texcoord":
                group.texcoord = resource;
                break;
        }
    }

    const groups: BufferGroup[] = [];
    for (const [key, group] of byKey) {
        if (
            !group.position?.filename ||
            !group.vector?.filename ||
            !group.blend?.filename ||
            !group.color?.filename ||
            !group.texcoord?.filename
        ) {
            warn(`Skipping incomplete WWMI buffer group: ${key}`);
            continue;
        }

        const resourcesToRead = [
            group.position,
            group.vector,
            group.blend,
            group.color,
            group.texcoord,
        ];
        const [position, vector, blend, color, texcoord] = await Promise.all(
            resourcesToRead.map((resource) => readResourceBytes(modDir, resource)),
        );

        const stride = resourcesToRead.reduce((sum, resource) => sum + (resource.stride ?? 0), 0);
        const vertexCount = Math.min(
            ...resourcesToRead.map((resource, index) =>
                Math.floor(
                    [position, vector, blend, color, texcoord][index].length /
                        (resource.stride ?? 1),
                ),
            ),
        );

        const vb = interleaveBufferSet(
            [
                { bytes: position, stride: group.position.stride! },
                { bytes: vector, stride: group.vector.stride! },
                { bytes: blend, stride: group.blend.stride! },
                { bytes: color, stride: group.color.stride! },
                { bytes: texcoord, stride: group.texcoord.stride! },
            ],
            vertexCount,
        );
        if (vb.length !== vertexCount * stride) {
            throw new Error(`Unexpected WWMI interleaved buffer length for ${key}`);
        }

        groups.push({ key, vbFilename: `${key}.vb`, vbBytes: vb, stride });
    }

    return groups;
}

export function parseMihoyoBufferGroupResourceName(
    resourceName: string,
): { key: string; kind: "position" | "blend" | "texcoord" } | null {
    const typedMatch = resourceName.match(/^(.*?)(Position|Blend|Texcoord)(\.\d+)?$/i);
    if (typedMatch) {
        const [, prefix, kind, suffix = ""] = typedMatch;
        return {
            key: `${prefix}${suffix}`,
            kind: kind.toLowerCase() as "position" | "blend" | "texcoord",
        };
    }

    const basePositionMatch = resourceName.match(/^(.*?)PositionBase(\.\d+)?$/i);
    if (basePositionMatch) {
        const [, prefix, suffix = ""] = basePositionMatch;
        return { key: `${prefix}${suffix}`, kind: "position" };
    }

    return null;
}

export function parseWwmiBufferResourceName(resourceName: string): {
    key: string;
    kind: "position" | "vector" | "blend" | "color" | "texcoord";
} | null {
    const match = resourceName.match(
        /^(.*?)(Position|Vector|Blend|Color|TexCoord)Buffer(\.\d+)?$/i,
    );
    if (!match) {
        return null;
    }

    const [, prefix, kind, suffix = ""] = match;
    return {
        key: `${prefix}IndexBuffer${suffix}`,
        kind: kind.toLowerCase() as "position" | "vector" | "blend" | "color" | "texcoord",
    };
}

function isShapeKeyPositionVariantResource(resourceName: string): boolean {
    return /Position(?!Base(?:\.|$))[\w.-]+$/i.test(resourceName);
}

function ensureMihoyoBufferResourceGroup(
    map: Map<string, MihoyoBufferResourceGroup>,
    key: string,
): MihoyoBufferResourceGroup {
    let value = map.get(key);
    if (!value) {
        value = {};
        map.set(key, value);
    }
    return value;
}

function ensureWwmiBufferResourceGroup(
    map: Map<string, WwmiBufferResourceGroup>,
    key: string,
): WwmiBufferResourceGroup {
    let value = map.get(key);
    if (!value) {
        value = {};
        map.set(key, value);
    }
    return value;
}

async function readResourceBytes(modDir: string, resource: Resource): Promise<Buffer> {
    const filePath = path.resolve(modDir, resource.filename!);
    if (!(await fse.pathExists(filePath))) {
        throw new Error(`Missing resource file: ${filePath}`);
    }
    return await fse.readFile(filePath);
}

function interleaveBufferSet(
    buffers: Array<{ bytes: Buffer; stride: number }>,
    vertexCount: number,
): Buffer {
    const stride = buffers.reduce((sum, entry) => sum + entry.stride, 0);
    const output = Buffer.allocUnsafe(vertexCount * stride);

    for (let vertexIndex = 0; vertexIndex < vertexCount; vertexIndex++) {
        let targetOffset = vertexIndex * stride;
        for (const entry of buffers) {
            const sourceOffset = vertexIndex * entry.stride;
            entry.bytes.copy(output, targetOffset, sourceOffset, sourceOffset + entry.stride);
            targetOffset += entry.stride;
        }
    }

    return output;
}

export function parseEfmiBufferResourceName(resourceName: string): {
    key: string;
    slot: "vb0" | "vb1" | "vb2";
} | null {
    const match = resourceName.match(/^(.*?)_(VB[012])(?:_LOD)?$/i);
    if (!match) return null;
    const [, prefix, slot] = match;
    return { key: prefix, slot: slot.toLowerCase() as "vb0" | "vb1" | "vb2" };
}

export async function collectEfmiBufferGroups(
    modDir: string,
    resources: Resource[],
    warn: (message: string) => void,
): Promise<BufferGroup[]> {
    const byKey = new Map<string, EfmiBufferResourceGroup>();

    for (const resource of resources) {
        if (!resource.filename || !resource.stride) continue;
        const typed = parseEfmiBufferResourceName(resource.name);
        if (!typed) continue;
        const group = ensureEfmiBufferResourceGroup(byKey, typed.key);
        if (typed.slot === "vb0") group.vb0 = resource;
        else if (typed.slot === "vb1") group.vb1 = resource;
        else group.vb2 = resource;
    }

    const groups: BufferGroup[] = [];
    for (const [key, group] of byKey) {
        if (!group.vb0?.filename || !group.vb1?.filename || !group.vb2?.filename) {
            continue;
        }

        const [vb0, vb1, vb2] = await Promise.all([
            readResourceBytes(modDir, group.vb0),
            readResourceBytes(modDir, group.vb1),
            readResourceBytes(modDir, group.vb2),
        ]);

        const items = [
            { bytes: vb0, stride: group.vb0.stride! },
            { bytes: vb1, stride: group.vb1.stride! },
            { bytes: vb2, stride: group.vb2.stride! },
        ];
        const stride = items.reduce((sum, item) => sum + item.stride, 0);
        const vertexCount = Math.min(
            ...items.map((item) => Math.floor(item.bytes.length / (item.stride || 1))),
        );

        const vb = interleaveBufferSet(items, vertexCount);
        if (vb.length !== vertexCount * stride) {
            throw new Error(`Unexpected EFMI interleaved buffer length for ${key}`);
        }

        groups.push({ key, vbFilename: `${key}.vb`, vbBytes: vb, stride });
    }

    return groups;
}

function ensureEfmiBufferResourceGroup(
    map: Map<string, EfmiBufferResourceGroup>,
    key: string,
): EfmiBufferResourceGroup {
    let value = map.get(key);
    if (!value) {
        value = {};
        map.set(key, value);
    }
    return value;
}
