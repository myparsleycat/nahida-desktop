import fsp from "node:fs/promises";
import path from "node:path";
import { getLockingProcesses } from "@native/native-fs";
import type { ProcessInfo } from "@native/native-fs";
import fg from "fast-glob";
import fse from "fs-extra";
import type { NahidaDesktop } from "..";

export interface FileNode {
    name: string;
    path: string;
    isDirectory: boolean;
    children?: FileNode[];
}

export interface ReaddirOptions {
    filter?: (name: string, isDirectory: boolean, fullPath: string) => boolean;
    mode?: "flat" | "tree";
}

export interface FileSearchOptions {
    extensions?: string[];
    limit?: number;
}

export interface FileWriteAccessResult {
    writable: boolean;
    exists: boolean;
    locked: boolean;
    processes: ProcessInfo[];
}

// oxlint-disable-next-line no-control-regex
const WINDOWS_INVALID_CHARS_REGEX = /[<>:"/\\|?*\u0000-\u001F]/;
// oxlint-disable-next-line no-control-regex
const WINDOWS_INVALID_CHARS_REGEX_GLOBAL = /[<>:"/\\|?*\u0000-\u001F]/g;
const WINDOWS_RESERVED_NAMES_REGEX = /^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\..*)?$/i;
const ONLY_DOTS_REGEX = /^\.+$/;
const TRAILING_DOTS_REGEX = /[.]+$/;

export class FS {
    private readonly desktop: NahidaDesktop;
    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public getUniqueName(name: string, existingNames: string[]) {
        const lowerNamesSet = new Set(existingNames.map((n) => n.toLowerCase()));
        let uniqueName = name;
        let counter = 1;

        while (lowerNamesSet.has(uniqueName.toLowerCase())) {
            counter++;
            uniqueName = `${name} (${counter})`;
        }

        return uniqueName;
    }

    public async isPathWritable(pathStr: string): Promise<boolean>;
    public async isPathWritable(
        pathStr: string,
        options: { detailed: true; parentPath?: string },
    ): Promise<FileWriteAccessResult>;
    public async isPathWritable(
        pathStr: string,
        options?: { detailed: true; parentPath?: string },
    ) {
        if (options?.detailed) {
            return this.getFileWriteAccess(pathStr, options.parentPath);
        }

        try {
            await fse.access(pathStr, fse.constants.W_OK | fse.constants.X_OK);
            return true;
        } catch {
            return false;
        }
    }

    private async getFileWriteAccess(filePath: string, parentPath = path.dirname(filePath)) {
        const fallbackResult: FileWriteAccessResult = {
            writable: false,
            exists: false,
            locked: false,
            processes: [],
        };

        try {
            await fse.access(parentPath, fse.constants.W_OK | fse.constants.X_OK);
        } catch {
            return fallbackResult;
        }

        const exists = await fse.pathExists(filePath);
        if (!exists) {
            return { ...fallbackResult, writable: true };
        }

        try {
            await fse.access(filePath, fse.constants.W_OK);
        } catch {
            const processes = await this.getLockingProcessesSafe(filePath);
            return {
                writable: false,
                exists,
                locked: processes.length > 0,
                processes,
            };
        }

        let handle: Awaited<ReturnType<typeof fsp.open>> | undefined;
        try {
            handle = await fsp.open(filePath, "r+");
            return {
                writable: true,
                exists,
                locked: false,
                processes: [],
            };
        } catch (error) {
            const lockInfo = await this.isLockedPathError(error, filePath);
            return {
                writable: false,
                exists,
                locked: lockInfo.isLocked,
                processes: lockInfo.processes,
            };
        } finally {
            await handle?.close().catch(() => {});
        }
    }

    public formatProcessList(processes: ProcessInfo[]) {
        return processes.map((proc) => `${proc.name} (${proc.pid})`).join(", ");
    }

    public async isPathReadable(pathStr: string) {
        try {
            await fse.access(pathStr, fse.constants.R_OK);
            return true;
        } catch {
            return false;
        }
    }

    public async rename(oldPath: fse.PathLike, newPath: fse.PathLike) {
        await fsp.rename(oldPath as string, newPath as string);
    }

    public async ensureDir(path: string, options?: number | fse.EnsureOptions | undefined) {
        await fse.ensureDir(path, options);
    }

    public async pathExists(path: string) {
        return fse.pathExists(path);
    }

    public async stat(path: fse.PathLike) {
        return fse.stat(path);
    }

    public async isLockedPathError(error: unknown, pathStr: string) {
        const code = (error as NodeJS.ErrnoException | undefined)?.code;
        const isLocked = code === "EBUSY" || code === "EPERM" || code === "EACCES";
        if (!isLocked) {
            return { isLocked: false, processes: [] };
        }

        const processes = await this.getLockingProcessesSafe(pathStr);
        return { isLocked, processes };
    }

    private async getLockingProcessesSafe(pathStr: string) {
        try {
            return await getLockingProcesses(pathStr);
        } catch (e) {
            this.desktop.logger.error(e, "FS:isLockedPathError:getLockingProcesses");
            return [];
        }
    }

    public isValidWindowsFilename(name: string): boolean {
        if (!name || name.length === 0 || name.length > 255) {
            return false;
        }

        // oxlint-disable-next-line no-control-regex
        if (WINDOWS_INVALID_CHARS_REGEX.test(name)) {
            return false;
        }

        if (ONLY_DOTS_REGEX.test(name)) {
            return false;
        }

        if (name.endsWith(" ") || name.endsWith(".")) {
            return false;
        }

        if (WINDOWS_RESERVED_NAMES_REGEX.test(name)) {
            return false;
        }

        return true;
    }

    public assertValidWindowsFilename(name: string) {
        if (!this.isValidWindowsFilename(name)) {
            throw new Error("INVALID_WINDOWS_FILENAME");
        }
    }

    public sanitizeWindowsFilename(input: string, sanitizeString = " ") {
        // oxlint-disable-next-line no-control-regex
        let sanitized = input.replace(WINDOWS_INVALID_CHARS_REGEX_GLOBAL, sanitizeString).trim();

        sanitized = sanitized.replace(TRAILING_DOTS_REGEX, "");

        if (sanitized.length === 0) {
            sanitized = "Untitled";
        }

        return sanitized;
    }

    public sanitizePath(input: string) {
        if (process.platform === "darwin") {
            const parsed = path.parse(input);

            if (!parsed.base) {
                return input;
            }

            const sanitizedBase = this.sanitizeWindowsFilename(parsed.base);

            if (!parsed.dir) {
                return sanitizedBase;
            }

            return path.join(parsed.dir, sanitizedBase);
        }

        return input
            .split(path.sep)
            .map((part, index) => {
                if (index === 0 && /^[a-zA-Z]:$/.test(part)) return part;
                return this.sanitizeWindowsFilename(part);
            })
            .join(path.sep);
    }

    public async readdirRecursive(
        dirPath: string,
        options: ReaddirOptions & { mode: "tree" },
    ): Promise<FileNode[]>;
    public async readdirRecursive(
        dirPath: string,
        options?: ReaddirOptions & { mode?: "flat" },
    ): Promise<string[]>;
    public async readdirRecursive(
        dirPath: string,
        options: ReaddirOptions = {},
    ): Promise<string[] | FileNode[]> {
        const { filter, mode = "flat" } = options;
        const entries = await fse.readdir(dirPath, { withFileTypes: true });

        const tasks = entries.map(async (entry): Promise<FileNode | null> => {
            const fullPath = path.join(dirPath, entry.name);
            const isDirectory = entry.isDirectory();

            if (filter && !filter(entry.name, isDirectory, fullPath)) {
                return null;
            }

            const node: FileNode = {
                name: entry.name,
                path: fullPath,
                isDirectory,
            };

            if (isDirectory) {
                const children = await this.readdirRecursive(fullPath, {
                    ...options,
                    mode: "tree",
                });
                node.children = children;
            }

            return node;
        });

        const nodes = (await Promise.all(tasks)).filter((node): node is FileNode => node !== null);

        if (mode === "tree") {
            return nodes;
        }

        return this.flattenNodes(nodes);
    }

    public async getFolderSize(path: string) {
        let totalSize = 0;

        try {
            const entries = await fg(["**/*"], {
                cwd: path,
                stats: true,
                dot: true,
                onlyFiles: true,
                absolute: true,
            });

            totalSize = entries.reduce((acc, entry) => acc + (entry.stats?.size ?? 0), 0);
        } catch (error) {
            this.desktop.logger.error(error, "FS:getFolderSize");
        }

        return totalSize;
    }

    public async listDirectories(dirPath: string) {
        const entries = await fse.readdir(dirPath, { withFileTypes: true });
        return entries.filter((entry) => entry.isDirectory()).map((entry) => entry.name);
    }

    public async findFiles(dirPath: string, options: FileSearchOptions = {}): Promise<string[]> {
        const { extensions, limit } = options;
        const normalizedExtensions = extensions?.map((ext) => ext.toLowerCase());
        const files = await fg(["**/*"], {
            cwd: dirPath,
            onlyFiles: true,
            absolute: true,
            dot: true,
        });
        const filteredFiles = files.filter((filePath) => {
            if (!normalizedExtensions || normalizedExtensions.length === 0) {
                return true;
            }
            return normalizedExtensions.includes(path.extname(filePath).toLowerCase());
        });

        if (typeof limit === "number") {
            return filteredFiles.slice(0, limit);
        }

        return filteredFiles;
    }

    private flattenNodes(nodes: FileNode[]): string[] {
        const result: string[] = [];

        for (const node of nodes) {
            result.push(node.path);
            if (node.children && node.children.length > 0) {
                result.push(...this.flattenNodes(node.children));
            }
        }

        return result;
    }
}
