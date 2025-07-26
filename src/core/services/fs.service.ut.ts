import { fileTypeFromFile } from "file-type";
import { FSService } from "./fs.service";
import crypto from 'node:crypto';

export interface DirInfo {
    uuid: string;
    parentId: string | null;
    name: string;
}

export interface FileInfo {
    uuid: string;
    fileId: string;
    parentId: string | null;
    name: string;
    size: number;
    compAlg?: "gzip" | "zstd" | null;
    url: string;
}

export interface DownloadData {
    totalBytes: number;
    files: FileInfo[];
    dirs: DirInfo[];
}

export function removeDuplicateDirs(dirs: DirInfo[]): DirInfo[] {
    const uniqueDirs: Record<string, DirInfo> = {};

    dirs.forEach(dir => {
        if (dir?.uuid && !uniqueDirs[dir.uuid]) {
            uniqueDirs[dir.uuid] = dir;
        }
    });

    return Object.values(uniqueDirs);
}

export function createDirMap(dirs: DirInfo[]): Record<string, DirInfo> {
    const dirMap: Record<string, DirInfo> = {};

    dirs.forEach(dir => {
        dirMap[dir.uuid] = dir;
    });

    return dirMap;
}

export async function createDirectoryStructure(
    rootPath: string,
    dirMap: Record<string, DirInfo>,
    rootId: string
): Promise<Record<string, string>> {
    const dirPaths: Record<string, string> = {};
    dirPaths[rootId] = rootPath;

    for (const uuid in dirMap) {
        const dir = dirMap[uuid];
        const dirPath = await calculateDirPath(rootPath, dir, dirMap, rootId, dirPaths);
        dirPaths[uuid] = dirPath;
    }

    for (const uuid in dirPaths) {
        if (uuid === rootId) continue; // 루트 디렉토리는 이미 생성됨
        await FSService.mkdir(dirPaths[uuid], { recursive: true });
        console.log("디렉토리 생성:", dirPaths[uuid]);
    }

    return dirPaths;
}

export async function calculateDirPath(
    rootPath: string,
    dir: DirInfo,
    dirMap: Record<string, DirInfo>,
    rootId: string,
    dirPaths: Record<string, string>
): Promise<string> {
    if (dirPaths[dir.uuid]) {
        return dirPaths[dir.uuid];
    }

    if (dir.uuid === rootId || !dir.parentId || dir.parentId === rootId) {
        const dirPath = `${rootPath}/${dir.name || dir.uuid}`;
        dirPaths[dir.uuid] = dirPath;
        return dirPath;
    }

    const parentDir = dirMap[dir.parentId];
    if (!parentDir) {
        // 부모를 찾을 수 없는 경우 루트에 생성
        const dirPath = `${rootPath}/${dir.name || dir.uuid}`;
        dirPaths[dir.uuid] = dirPath;
        return dirPath;
    }

    const parentPath = await calculateDirPath(rootPath, parentDir, dirMap, rootId, dirPaths);
    const dirPath = `${parentPath}/${dir.name || dir.uuid}`;
    dirPaths[dir.uuid] = dirPath;
    return dirPath;
}

export async function calculateSHA256(filePath: string) {
    const buffer = await FSService.readFile(filePath, 'buf');
    return crypto.createHash('sha256').update(buffer).digest('hex');
}

export async function getFileType(path: string) {
    try {
        const fileType = await fileTypeFromFile(path);
        return fileType || null;
    } catch {
        return null;
    }
}