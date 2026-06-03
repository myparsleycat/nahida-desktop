import crypto from "crypto";
import path from "path";
import { mapAsync } from "es-toolkit/array";
import fg from "fast-glob";
import fse from "fs-extra";

export type FloatArray = Float32Array;

export interface FileFingerprint {
    path: string;
    size: number;
    exactHash?: string;
    vector: FloatArray;
}

export interface FolderFingerprint {
    folderPath: string;
    fileCount: number;
    totalSize: number;
    vector: FloatArray;
    simhashHex: string;
}

export interface CompareResult {
    cosine: number;
    hammingDistance: number;
    hammingSimilarity: number;
    combinedScore: number;
}

export interface BuildOptions {
    vectorDim?: number;
    simhashBits?: number;
    maxFiles?: number;
    maxFileSizeBytes?: number;
    minFileSizeBytes?: number;
    sampleBudgetBytes?: number;
    allowedExtensions?: Set<string> | null;
    computeExactHash?: boolean;
    concurrency?: number;
}

export interface CompareOptions {
    cosineWeight?: number;
    hammingWeight?: number;
}

const BYTE_HIST_BINS = 32;
const BIGRAM_BINS = 64;
const STAT_FEATURES = 8;

const TARGET_EXTENSIONS = new Set([
    ".ini",
    ".vb",
    ".ib",
    ".fmt",
    ".buf",
    ".dds",
    ".assets",
    ".hlsl",
    ".bin",
]);

const DEFAULT_BUILD_OPTIONS: Required<BuildOptions> = {
    vectorDim: 128,
    simhashBits: 128,
    maxFiles: Number.POSITIVE_INFINITY,
    maxFileSizeBytes: 512 * 1024 * 1024,
    minFileSizeBytes: 1,
    sampleBudgetBytes: 64 * 1024,
    allowedExtensions: TARGET_EXTENSIONS,
    computeExactHash: false,
    concurrency: 8,
};

const DEFAULT_COMPARE_OPTIONS: Required<CompareOptions> = {
    cosineWeight: 0.5,
    hammingWeight: 0.5,
};

function l2Norm(v: FloatArray): number {
    let sum = 0;
    for (let i = 0; i < v.length; i++) sum += v[i] * v[i];
    return Math.sqrt(sum);
}

function normalizeInPlace(v: FloatArray): FloatArray {
    const norm = l2Norm(v);
    if (norm > 1e-12) {
        for (let i = 0; i < v.length; i++) v[i] /= norm;
    }
    return v;
}

function fnv1a32(str: string): number {
    let h = 0x811c9dc5;
    for (let i = 0; i < str.length; i++) {
        h ^= str.charCodeAt(i);
        h = Math.imul(h, 0x01000193);
    }
    return h >>> 0;
}

function hashFeatureToDim(featureKey: string, dim: number): { index: number; sign: number } {
    const h = fnv1a32(featureKey);
    return {
        index: h % dim,
        sign: ((h >>> 31) & 1) === 0 ? 1 : -1,
    };
}

function addHashedFeature(vec: FloatArray, key: string, value: number): void {
    if (value === 0) return;
    const { index, sign } = hashFeatureToDim(key, vec.length);
    vec[index] += sign * value;
}

function shannonEntropyFromCounts(counts: number[], total: number): number {
    if (total <= 0) return 0;
    let entropy = 0;
    for (const c of counts) {
        if (c <= 0) continue;
        const p = c / total;
        entropy -= p * Math.log2(p);
    }
    return entropy;
}

function chooseSampleWindows(
    fileSize: number,
    sampleBudget: number,
): Array<{ start: number; length: number }> {
    if (fileSize <= 0) return [];

    const windowCount = Math.max(1, Math.min(8, Math.floor(sampleBudget / 4096) || 1));

    if (fileSize <= sampleBudget) {
        return [{ start: 0, length: fileSize }];
    }

    const baseWindowSize = Math.max(1024, Math.floor(sampleBudget / windowCount));
    const maxNonOverlappingWindowSize = Math.max(1, Math.floor(fileSize / windowCount));
    const windowSize = Math.min(baseWindowSize, maxNonOverlappingWindowSize);

    const windows: Array<{ start: number; length: number }> = [];
    for (let i = 0; i < windowCount; i++) {
        const segmentStart = Math.floor((fileSize * i) / windowCount);
        const segmentEnd = Math.floor((fileSize * (i + 1)) / windowCount);
        const segmentLength = Math.max(1, segmentEnd - segmentStart);
        const length = Math.min(windowSize, segmentLength);
        const start = segmentStart + Math.max(0, Math.floor((segmentLength - length) / 2));
        windows.push({ start, length });
    }

    return windows;
}

async function sampleFileBytes(
    filePath: string,
    fileSize: number,
    sampleBudget: number,
): Promise<Buffer> {
    const fd = await fse.open(filePath, "r");
    try {
        const windows = chooseSampleWindows(fileSize, sampleBudget);
        const parts: Buffer[] = [];

        for (const w of windows) {
            const buf = Buffer.allocUnsafe(w.length);
            const { bytesRead } = await fse.read(fd, buf, 0, w.length, w.start);
            parts.push(buf.subarray(0, bytesRead));
        }

        return Buffer.concat(parts);
    } finally {
        await fse.close(fd);
    }
}

async function sha256FileStream(filePath: string): Promise<string> {
    const hash = crypto.createHash("sha256");
    const stream = fse.createReadStream(filePath);
    for await (const chunk of stream) {
        hash.update(chunk as Buffer);
    }
    return hash.digest("hex");
}

function buildBinaryFeatureVector(sample: Buffer, fileSize: number, dim: number): FloatArray {
    const out = new Float32Array(dim);
    if (sample.length === 0) return out;

    const byteHist = new Array<number>(BYTE_HIST_BINS).fill(0);
    const bigramHist = new Array<number>(BIGRAM_BINS).fill(0);

    let sum = 0;
    let sumSq = 0;
    let transitions = 0;
    let zeroCount = 0;
    let asciiCount = 0;
    let highByteCount = 0;

    for (let i = 0; i < sample.length; i++) {
        const b = sample[i];
        sum += b;
        sumSq += b * b;

        if (b === 0) zeroCount++;
        if (b >= 32 && b <= 126) asciiCount++;
        if (b >= 128) highByteCount++;

        const histBin = Math.floor((b / 256) * BYTE_HIST_BINS);
        byteHist[Math.min(BYTE_HIST_BINS - 1, histBin)]++;

        if (i > 0) {
            const prev = sample[i - 1];
            if (prev !== b) transitions++;
            const bg = ((prev << 8) | b) >>> 0;
            bigramHist[bg % BIGRAM_BINS]++;
        }
    }

    const n = sample.length;
    const mean = sum / n;
    const variance = Math.max(0, sumSq / n - mean * mean);
    const stddev = Math.sqrt(variance);
    const entropy = shannonEntropyFromCounts(byteHist, n);
    const transitionRate = n > 1 ? transitions / (n - 1) : 0;
    const zeroRatio = zeroCount / n;
    const asciiRatio = asciiCount / n;
    const highByteRatio = highByteCount / n;
    const sampleToFileRatio = Math.min(1, n / Math.max(1, fileSize));
    const logFileSize = Math.log1p(fileSize);

    for (let i = 0; i < BYTE_HIST_BINS; i++) {
        addHashedFeature(out, `bh:${i}`, byteHist[i] / n);
    }

    const bgTotal = Math.max(1, n - 1);
    for (let i = 0; i < BIGRAM_BINS; i++) {
        addHashedFeature(out, `bg:${i}`, bigramHist[i] / bgTotal);
    }

    const stats = [
        mean / 255,
        stddev / 128,
        entropy / Math.log2(BYTE_HIST_BINS),
        transitionRate,
        zeroRatio,
        asciiRatio,
        highByteRatio,
        sampleToFileRatio,
    ];

    for (let i = 0; i < STAT_FEATURES; i++) {
        addHashedFeature(out, `st:${i}`, stats[i] ?? 0);
    }

    const chunkSize = Math.max(256, Math.floor(n / 16));
    for (let start = 0, idx = 0; start < n; start += chunkSize, idx++) {
        const end = Math.min(n, start + chunkSize);
        const chunk = sample.subarray(start, end);
        const chunkHash = crypto.createHash("sha1").update(chunk).digest("hex").slice(0, 16);
        addHashedFeature(out, `ck:${idx}:${chunkHash}`, 1.0);
    }

    addHashedFeature(out, `sz:bucket:${Math.min(31, Math.floor(logFileSize))}`, 0.25);

    return normalizeInPlace(out);
}

function averageNormalizedVectors(vectors: FloatArray[], dim: number): FloatArray {
    const out = new Float32Array(dim);
    if (vectors.length === 0) return out;

    for (const v of vectors) {
        for (let i = 0; i < dim; i++) {
            out[i] += v[i];
        }
    }

    const inv = 1 / vectors.length;
    for (let i = 0; i < dim; i++) {
        out[i] *= inv;
    }

    return normalizeInPlace(out);
}

function popcount8(n: number): number {
    n &= 0xff;
    let c = 0;
    while (n) {
        n &= n - 1;
        c++;
    }
    return c;
}

async function collectFiles(root: string, maxFiles: number): Promise<string[]> {
    const files: string[] = [];
    const stream = fg.stream("**/*", { cwd: root, absolute: true, onlyFiles: true });

    for await (const entry of stream) {
        files.push(entry as string);
        if (files.length >= maxFiles) break;
    }

    return files;
}

export class FingerprintService {
    private readonly defaultBuildOptions: Required<BuildOptions>;
    private readonly defaultCompareOptions: Required<CompareOptions>;

    constructor(buildOptions?: BuildOptions, compareOptions?: CompareOptions) {
        this.defaultBuildOptions = this.mergeBuildOptions(DEFAULT_BUILD_OPTIONS, buildOptions);
        this.defaultCompareOptions = this.mergeCompareOptions(
            DEFAULT_COMPARE_OPTIONS,
            compareOptions,
        );
    }

    private mergeBuildOptions(
        base: Required<BuildOptions>,
        overrides?: BuildOptions,
    ): Required<BuildOptions> {
        const merged: Required<BuildOptions> = {
            ...base,
            ...overrides,
            allowedExtensions:
                overrides && "allowedExtensions" in overrides
                    ? (overrides.allowedExtensions ?? null)
                    : base.allowedExtensions,
        };

        if (merged.vectorDim < 64) {
            throw new Error(`vectorDim must be >= 64, got ${merged.vectorDim}`);
        }
        if (merged.simhashBits < 64 || merged.simhashBits % 8 !== 0) {
            throw new Error(
                `simhashBits must be a multiple of 8 and >= 64, got ${merged.simhashBits}`,
            );
        }
        if (merged.vectorDim < merged.simhashBits) {
            throw new Error(
                `vectorDim (${merged.vectorDim}) must be >= simhashBits (${merged.simhashBits})`,
            );
        }
        if (merged.concurrency < 1) {
            throw new Error(`concurrency must be >= 1, got ${merged.concurrency}`);
        }

        return merged;
    }

    private mergeCompareOptions(
        base: Required<CompareOptions>,
        overrides?: CompareOptions,
    ): Required<CompareOptions> {
        const merged = { ...base, ...overrides };
        const sum = merged.cosineWeight + merged.hammingWeight;
        if (sum <= 0) {
            throw new Error("cosineWeight + hammingWeight must be > 0");
        }
        merged.cosineWeight /= sum;
        merged.hammingWeight /= sum;
        return merged;
    }

    public async fingerprintFile(
        filePath: string,
        options?: BuildOptions,
    ): Promise<FileFingerprint | null> {
        const opt = this.mergeBuildOptions(this.defaultBuildOptions, options);
        const st = await fse.stat(filePath);

        if (!st.isFile()) return null;
        if (st.size < opt.minFileSizeBytes) return null;
        if (st.size > opt.maxFileSizeBytes) return null;

        if (opt.allowedExtensions) {
            const ext = path.extname(filePath).toLowerCase();
            if (!opt.allowedExtensions.has(ext)) return null;
        }

        const sample = await sampleFileBytes(filePath, st.size, opt.sampleBudgetBytes);
        const vector = buildBinaryFeatureVector(sample, st.size, opt.vectorDim);

        const fp: FileFingerprint = {
            path: filePath,
            size: st.size,
            vector,
        };

        if (opt.computeExactHash) {
            fp.exactHash = await sha256FileStream(filePath);
        }

        return fp;
    }

    public async buildFolderFingerprint(
        folderPath: string,
        options?: BuildOptions,
    ): Promise<FolderFingerprint> {
        const opt = this.mergeBuildOptions(this.defaultBuildOptions, options);
        const selectedFiles = await collectFiles(folderPath, opt.maxFiles);

        const results = await mapAsync(
            selectedFiles,
            async (filePath) => {
                try {
                    const value = await this.fingerprintFile(filePath, opt);
                    return {
                        status: "fulfilled",
                        value,
                    } as PromiseSettledResult<FileFingerprint | null>;
                } catch (reason) {
                    return {
                        status: "rejected",
                        reason,
                    } as PromiseSettledResult<FileFingerprint | null>;
                }
            },
            { concurrency: opt.concurrency },
        );

        const fileFingerprints: FileFingerprint[] = [];
        let totalSize = 0;

        for (const result of results) {
            if (result.status !== "fulfilled") continue;
            const fp = result.value;
            if (!fp) continue;
            fileFingerprints.push(fp);
            totalSize += fp.size;
        }

        const folderVector = averageNormalizedVectors(
            fileFingerprints.map((f) => f.vector),
            opt.vectorDim,
        );

        const simhashHex = FingerprintService.vectorToSimhashHex(folderVector, opt.simhashBits);

        return {
            folderPath,
            fileCount: fileFingerprints.length,
            totalSize,
            vector: folderVector,
            simhashHex,
        };
    }

    public compareFingerprints(
        a: FolderFingerprint,
        b: FolderFingerprint,
        options?: CompareOptions,
    ): CompareResult {
        const opt = this.mergeCompareOptions(this.defaultCompareOptions, options);

        const cosine = FingerprintService.cosineSimilarity(a.vector, b.vector);
        const hammingDistance = FingerprintService.hammingDistanceHex(a.simhashHex, b.simhashHex);
        const bitCount = Buffer.from(a.simhashHex, "hex").length * 8;
        const hammingSimilarity = 1 - hammingDistance / bitCount;
        const combinedScore = opt.cosineWeight * cosine + opt.hammingWeight * hammingSimilarity;

        return {
            cosine,
            hammingDistance,
            hammingSimilarity,
            combinedScore,
        };
    }

    public serialize(fp: FolderFingerprint): string {
        return JSON.stringify({
            folderPath: fp.folderPath,
            fileCount: fp.fileCount,
            totalSize: fp.totalSize,
            vector: Array.from(fp.vector),
            simhashHex: fp.simhashHex,
        });
    }

    public deserialize(json: string): FolderFingerprint {
        const o = JSON.parse(json);
        return {
            folderPath: String(o.folderPath),
            fileCount: Number(o.fileCount),
            totalSize: Number(o.totalSize),
            vector: Float32Array.from(o.vector as number[]),
            simhashHex: String(o.simhashHex),
        };
    }

    public static cosineSimilarity(a: FloatArray, b: FloatArray): number {
        if (a.length !== b.length) {
            throw new Error(`Dimension mismatch: ${a.length} vs ${b.length}`);
        }

        let dot = 0;
        let na = 0;
        let nb = 0;
        for (let i = 0; i < a.length; i++) {
            dot += a[i] * b[i];
            na += a[i] * a[i];
            nb += b[i] * b[i];
        }

        if (na <= 1e-12 || nb <= 1e-12) return 0;
        return dot / (Math.sqrt(na) * Math.sqrt(nb));
    }

    public static vectorToSimhashHex(v: FloatArray, bitCount = 128): string {
        if (bitCount % 8 !== 0 || bitCount < 64) {
            throw new Error("bitCount must be a multiple of 8 and >= 64");
        }
        if (v.length < bitCount) {
            throw new Error(`vector dimension ${v.length} must be >= bitCount ${bitCount}`);
        }

        const bytes = Buffer.alloc(bitCount / 8, 0);
        for (let i = 0; i < bitCount; i++) {
            const bit = v[i] >= 0 ? 1 : 0;
            const byteIndex = Math.floor(i / 8);
            const bitIndex = 7 - (i % 8);
            if (bit) bytes[byteIndex] |= 1 << bitIndex;
        }
        return bytes.toString("hex");
    }

    public static hammingDistanceHex(aHex: string, bHex: string): number {
        const a = Buffer.from(aHex, "hex");
        const b = Buffer.from(bHex, "hex");
        if (a.length !== b.length) {
            throw new Error(`Simhash length mismatch: ${a.length} vs ${b.length}`);
        }

        let dist = 0;
        for (let i = 0; i < a.length; i++) {
            dist += popcount8(a[i] ^ b[i]);
        }
        return dist;
    }
}
