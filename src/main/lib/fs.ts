import fsp from "node:fs/promises";
import path from "node:path";
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

export class FS {
    private readonly desktop: NahidaDesktop;

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async isPathWritable(pathStr: string) {
        try {
            await fse.access(pathStr, fse.constants.W_OK | fse.constants.R_OK);
            return true;
        } catch {
            return false;
        }
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

    public sanitizeWindowsFilename(input: string) {
        const reservedChars = /[<>:"/\\|?*]/g;
        return input.replace(reservedChars, "_");
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
