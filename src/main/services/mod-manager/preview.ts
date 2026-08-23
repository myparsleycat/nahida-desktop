import path from "node:path";

import fse from "fs-extra";

import { isDisabledFolderName, stripDisabledFileSuffix } from "./path-utils";

const MEDIA_EXTENSIONS = new Set([
    "png",
    "jpg",
    "jpeg",
    "gif",
    "webp",
    "bmp",
    "avif",
    "avifs",
    "mp4",
    "webm",
    "avi",
    "mkv",
    "mov",
    "ogg",
]);
const VIDEO_SCORE_EXTENSIONS = new Set(["mp4", "webm"]);
const EXCLUDED_NAME_FRAGMENTS = ["normal", "light", "material", "diffuse"] as const;

const PREVIEW_NAME_PREFIX_SCORE = 1000;
const PREVIEW_NAME_CONTAINS_SCORE = 500;
const ROOT_FILE_SCORE = 200;
const VIDEO_FILE_SCORE = 10;

type PreviewLocation = "root" | "enabled" | "disabled";

type PreviewCandidate = {
    score: number;
    path: string;
    location: PreviewLocation;
};

export async function findPreview(dirPath: string, searchSubfolders: boolean) {
    const candidates = (await collectFiles(dirPath, searchSubfolders)).flatMap((filePath) => {
        const candidate = toPreviewCandidate(dirPath, filePath);
        return candidate ? [candidate] : [];
    });

    return pickBestCandidate(candidates)?.path;
}

async function collectFiles(dirPath: string, searchSubfolders: boolean): Promise<string[]> {
    if (!(await fse.pathExists(dirPath))) return [];

    const nested = await Promise.all(
        (await fse.readdir(dirPath, { withFileTypes: true })).map(async (entry) => {
            const entryPath = path.join(dirPath, entry.name);
            if (entry.isFile()) return [entryPath];
            if (!searchSubfolders || !entry.isDirectory()) return [];
            return collectFiles(entryPath, true);
        }),
    );

    return nested.flat();
}

function toPreviewCandidate(rootPath: string, filePath: string): PreviewCandidate | undefined {
    const activeName = stripDisabledFileSuffix(path.basename(filePath));
    const extension = path.extname(activeName).slice(1).toLowerCase();
    if (!MEDIA_EXTENSIONS.has(extension)) return undefined;

    const lowerName = activeName.toLowerCase();
    if (EXCLUDED_NAME_FRAGMENTS.some((fragment) => lowerName.includes(fragment))) return undefined;

    const relativeSegments = path
        .relative(rootPath, filePath)
        .split(/[\\/]+/)
        .filter(Boolean);
    if (relativeSegments.length === 0) return undefined;

    const isRoot = relativeSegments.length === 1;
    return {
        score: previewScore(lowerName, isRoot, VIDEO_SCORE_EXTENSIONS.has(extension)),
        path: filePath,
        location: isRoot
            ? "root"
            : relativeSegments.slice(0, -1).some(isDisabledFolderName)
              ? "disabled"
              : "enabled",
    };
}

function previewScore(fileName: string, isRoot: boolean, isVideo: boolean) {
    const nameScore = fileName.startsWith("preview")
        ? PREVIEW_NAME_PREFIX_SCORE
        : fileName.includes("preview")
          ? PREVIEW_NAME_CONTAINS_SCORE
          : 0;

    return nameScore + (isRoot ? ROOT_FILE_SCORE : 0) + (isVideo ? VIDEO_FILE_SCORE : 0);
}

function pickBestCandidate(candidates: PreviewCandidate[]) {
    if (candidates.length === 0) return undefined;

    return candidates.reduce((best, candidate) => {
        const locationDelta = locationRank(candidate.location) - locationRank(best.location);
        if (locationDelta !== 0) return locationDelta < 0 ? candidate : best;
        if (candidate.score !== best.score) return candidate.score > best.score ? candidate : best;
        return candidate.path.localeCompare(best.path, undefined, { numeric: true }) < 0
            ? candidate
            : best;
    });
}

function locationRank(location: PreviewLocation) {
    if (location === "root") return 0;
    if (location === "enabled") return 1;
    return 2;
}
