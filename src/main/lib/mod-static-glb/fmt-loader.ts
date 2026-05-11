import path from "node:path";
import { decodeIndices as decodeIndicesNative } from "@native/static-glb";
import fg from "fast-glob";
import fse from "fs-extra";
import { normalizeKey } from "./shared";
import type {
    FmtElement,
    FmtLayout,
    IbResource,
    StaticGlbBuildContext,
    StaticGlbModLayout,
} from "./types";

export async function loadFmtForIbCached(
    context: StaticGlbBuildContext,
    assetPath: string,
    ib: IbResource,
    stride: number,
): Promise<FmtLayout> {
    const cacheKey = `${context.layout}:${normalizeKey(ib.filename)}:${stride}:${normalizeKey(ib.key)}`;
    const cached = context.fmtByIbKey.get(cacheKey);
    if (cached) {
        return cached;
    }

    const request = loadFmtForIb(context.modDir, assetPath, ib, stride, context.layout).catch(
        (error) => {
            context.fmtByIbKey.delete(cacheKey);
            throw error;
        },
    );
    context.fmtByIbKey.set(cacheKey, request);
    return request;
}

export async function loadIndicesForIbCached(
    context: StaticGlbBuildContext,
    ib: IbResource,
    format: string,
): Promise<Uint32Array> {
    const ibPath = path.resolve(context.modDir, ib.filename);
    const cacheKey = `${ibPath}:${format}`;
    const cached = context.indicesByIbKey.get(cacheKey);
    if (cached) {
        return cached;
    }

    const request = fse
        .readFile(ibPath)
        .then((bytes) => decodeIndices(bytes, format))
        .catch((error) => {
            context.indicesByIbKey.delete(cacheKey);
            throw error;
        });
    context.indicesByIbKey.set(cacheKey, request);
    return request;
}

export async function loadFmtForIb(
    modDir: string,
    assetDir: string,
    ib: IbResource,
    stride: number,
    layout: StaticGlbModLayout,
): Promise<FmtLayout> {
    const stem = path.basename(ib.filename, path.extname(ib.filename));
    const localFmt = path.resolve(modDir, `${stem}.fmt`);
    if (await fse.pathExists(localFmt)) {
        return parseFmt(await fse.readFile(localFmt, "utf8"), stride, ib.format);
    }

    const assetFmt = await findRecursive(assetDir, "**/*.fmt", (file) => {
        const lower = path.basename(file).toLowerCase();
        return normalizeKey(lower).includes(normalizeKey(stem));
    });
    if (assetFmt) {
        return parseFmt(await fse.readFile(assetFmt, "utf8"), stride, ib.format);
    }

    if (layout === "wwmi" || layout === "efmi") {
        const hashFmt = await findHashBasedFmtForIb(assetDir, ib, stride);
        if (hashFmt) {
            return hashFmt;
        }
    }

    let vb0Txt = await findRecursive(assetDir, "**/*.txt", (file) => {
        const lower = path.basename(file).toLowerCase();
        return lower.includes("vb0") && normalizeKey(lower).includes(normalizeKey(stem));
    });

    if (!vb0Txt) {
        const hashCandidates = Array.from(
            new Set(
                [...(ib.overrideHashes ?? []), ib.overrideHash, ib.key]
                    .map((value) => normalizeKey(value || ""))
                    .filter(Boolean),
            ),
        );

        for (const ibHash of hashCandidates) {
            const ibTxt = await findRecursive(assetDir, "**/*.txt", (file) => {
                const lower = path.basename(file).toLowerCase();
                return lower.includes("-ib=") && normalizeKey(lower).includes(ibHash);
            });

            if (!ibTxt) {
                continue;
            }

            const ibBase = path.basename(ibTxt).replace(/-ib=.*$/i, "");
            vb0Txt = await findRecursive(assetDir, "**/*.txt", (file) => {
                const lower = path.basename(file).toLowerCase();
                return lower.includes("vb0") && lower.startsWith(ibBase.toLowerCase());
            });

            if (vb0Txt) {
                break;
            }
        }
    }

    if (!vb0Txt) {
        throw new Error(`No matching .fmt or *-vb0.txt found for ${ib.filename} under ${assetDir}`);
    }

    return parseFmt(
        extractFmtFromVb0(await fse.readFile(vb0Txt, "utf8"), stride, ib.format),
        stride,
        ib.format,
    );
}

async function findHashBasedFmtForIb(
    assetDir: string,
    ib: IbResource,
    stride: number,
): Promise<FmtLayout | null> {
    const hashCandidates = Array.from(
        new Set(
            [...(ib.overrideHashes ?? []), ib.overrideHash, ib.key]
                .map((value) => normalizeKey(value || ""))
                .filter(Boolean),
        ),
    );

    for (const candidate of hashCandidates) {
        const fmtPaths = await fg("**/*.fmt", {
            cwd: path.resolve(assetDir),
            absolute: true,
            onlyFiles: true,
            caseSensitiveMatch: false,
        });
        const matching = fmtPaths.filter((file) => normalizeKey(file).includes(candidate));
        if (matching.length === 0) {
            continue;
        }

        const parsed = await Promise.all(
            matching.map(async (file) => ({
                file,
                fmt: parseFmt(await fse.readFile(file, "utf8"), stride, ib.format),
            })),
        );
        parsed.sort((left, right) => left.fmt.stride - right.fmt.stride);
        return parsed[0]?.fmt ?? null;
    }

    return null;
}

export async function findRecursive(
    root: string,
    pattern: string | string[],
    predicate: (file: string) => boolean,
): Promise<string | null> {
    const matches = await fg(pattern, {
        cwd: path.resolve(root),
        absolute: true,
        onlyFiles: true,
        caseSensitiveMatch: false,
    });

    return matches.find((file) => predicate(file)) ?? null;
}

export function extractFmtFromVb0(text: string, stride: number, indexFormat: string): string {
    const lines = text.split(/\r?\n/).map((line) => line.trim());
    const out = [`stride: ${stride}`, "topology: trianglelist", `format: ${indexFormat}`];
    for (let i = 0; i < lines.length; i++) {
        if (!lines[i].startsWith("element[")) continue;
        out.push(lines[i]);
        for (let j = 1; j <= 7 && i + j < lines.length; j++) {
            out.push(`  ${lines[i + j]}`);
        }
    }
    return out.join("\n");
}

export function parseFmt(
    text: string,
    fallbackStride: number,
    fallbackIndexFormat: string,
): FmtLayout {
    const lines = text.split(/\r?\n/);
    const layout: FmtLayout = {
        stride: fallbackStride,
        topology: "trianglelist",
        indexFormat: fallbackIndexFormat || "DXGI_FORMAT_R32_UINT",
        elements: [],
    };
    let current: Partial<FmtElement> | null = null;
    let appendOffset = 0;

    for (const raw of lines) {
        const line = raw.trim();
        if (!line) continue;
        if (line.startsWith("stride:")) layout.stride = Number(line.slice("stride:".length).trim());
        else if (line.startsWith("topology:"))
            layout.topology = line.slice("topology:".length).trim();
        else if (line.startsWith("format:"))
            layout.indexFormat = line.slice("format:".length).trim();
        else if (line.startsWith("element[")) {
            if (current) {
                const element = completeElement(current);
                layout.elements.push(element);
                appendOffset = element.alignedByteOffset + formatByteSize(element.format);
            }
            current = {};
        } else if (current) {
            const sep = line.indexOf(":");
            if (sep < 0) continue;
            const key = line.slice(0, sep).trim();
            const value = line.slice(sep + 1).trim();
            switch (key) {
                case "SemanticName":
                    current.semanticName = value;
                    break;
                case "SemanticIndex":
                    current.semanticIndex = Number(value);
                    break;
                case "Format":
                    current.format = value;
                    break;
                case "InputSlot":
                    current.inputSlot = Number(value);
                    break;
                case "AlignedByteOffset":
                    current.alignedByteOffset = value === "append" ? appendOffset : Number(value);
                    break;
                case "InputSlotClass":
                    current.inputSlotClass = value;
                    break;
                case "InstanceDataStepRate":
                    current.instanceDataStepRate = Number(value);
                    break;
            }
        }
    }
    if (current) layout.elements.push(completeElement(current));
    layout.elements = layout.elements.filter(
        (element) => element.inputSlotClass !== "per-instance",
    );
    return layout;
}

function completeElement(value: Partial<FmtElement>): FmtElement {
    return {
        semanticName: value.semanticName || "",
        semanticIndex: value.semanticIndex || 0,
        format: value.format || "DXGI_FORMAT_UNKNOWN",
        inputSlot: value.inputSlot || 0,
        alignedByteOffset: value.alignedByteOffset || 0,
        inputSlotClass: value.inputSlotClass || "per-vertex",
        instanceDataStepRate: value.instanceDataStepRate || 0,
    };
}

export function decodeIndices(bytes: Buffer, format: string): Uint32Array {
    return bufferToUint32Array(decodeIndicesNative(bytes, format));
}

function bufferToUint32Array(buffer: Buffer): Uint32Array {
    return new Uint32Array(
        buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength),
    );
}

function formatByteSize(format: string): number {
    const upper = format.toUpperCase();
    if (upper === "DXGI_FORMAT_R10G10B10A2_UNORM") return 4;
    const count = formatComponentCount(upper);
    if (upper.includes("32")) return count * 4;
    if (upper.includes("16")) return count * 2;
    if (upper.includes("8")) return count;
    return 0;
}

function formatComponentCount(format: string): number {
    const normalized = format.toUpperCase().replace(/^DXGI_FORMAT_/, "");
    const channels = normalized.match(/[RGBA]\d+/g);
    return channels ? channels.length : 1;
}
