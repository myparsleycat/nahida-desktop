import { exec } from "node:child_process";
import net from "node:net";
import pathModule from "node:path";
import FastGlob, { type Entry } from "fast-glob";
import fs from "fs-extra";

export async function promiseAllChunked<T>(
    promises: Promise<T>[],
    chunkSize = 10000,
): Promise<T[]> {
    const results: T[] = [];

    for (let i = 0; i < promises.length; i += chunkSize) {
        const chunkResults = await Promise.all(promises.slice(i, i + chunkSize));

        results.push(...chunkResults);
    }

    return results;
}

export async function isPortInUse(port: number): Promise<boolean> {
    return new Promise((resolve, reject) => {
        const server = net.createServer();

        server.once("error", (err: NodeJS.ErrnoException) => {
            if (err.code === "EADDRINUSE") {
                resolve(true);

                return;
            }

            reject(err);
        });

        server.once("listening", () => {
            server.close(() => {
                resolve(false);
            });
        });

        server.listen(port);
    });
}

export function canStartServerOnIPAndPort(ip: string, port: number): Promise<boolean> {
    return new Promise((resolve) => {
        const server = net.createServer();

        server.once("error", () => {
            server.close();

            resolve(false);
        });

        server.once("listening", () => {
            server.close();

            resolve(true);
        });

        server.listen(port, ip);
    });
}

export async function execCommand(command: string, trimStdOut: boolean = true): Promise<string> {
    return new Promise((resolve, reject) => {
        exec(
            command,
            {
                shell: "cmd.exe",
            },
            (err, stdout, stderr) => {
                if (err || stderr) {
                    reject(err ? err : new Error(stderr));

                    return;
                }

                resolve(trimStdOut ? stdout.trim() : stdout);
            },
        );
    });
}

export type SerializedError = {
    name: string;
    message: string;
    stack?: string;
    stringified: string;
};

export function serializeError(error: Error): SerializedError {
    return {
        name: error.name,
        message: error.message,
        stack: error.stack,
        stringified: JSON.stringify(error),
    };
}

export function deserializeError(serializedError: SerializedError): Error {
    const error = new Error(serializedError.message);

    error.name = serializedError.name;
    error.stack = serializedError.stack;

    return error;
}

export async function isProcessRunning(processName: string): Promise<boolean> {
    return await new Promise<boolean>((resolve, reject) => {
        const command = `tasklist /FI "IMAGENAME eq ${processName}"`;

        exec(command, (err, stdout, stderr) => {
            if (err) {
                reject(false);

                return;
            }

            if (stderr) {
                reject(false);

                return;
            }

            resolve(stdout.trim().length > 0);
        });
    });
}

/**
 * Parse the requested byte range from the header.
 *
 * @export
 * @param {string} range
 * @param {number} totalLength
 * @returns {({ start: number; end: number } | null)}
 */
export function parseByteRange(
    range: string,
    totalLength: number,
): { start: number; end: number } | null {
    const [unit, rangeValue] = range.split("=");

    if (unit !== "bytes" || !rangeValue) {
        return null;
    }

    const [startStr, endStr] = rangeValue.split("-");

    if (!startStr) {
        return null;
    }

    const start = parseInt(startStr, 10);
    const end = endStr ? parseInt(endStr, 10) : totalLength - 1;

    if (isNaN(start) || isNaN(end) || start < 0 || end >= totalLength || start > end) {
        return null;
    }

    return {
        start,
        end,
    };
}

export type DriveInfo = {
    isPhysical: boolean;
    isExternal: boolean;
};

// eslint-disable-next-line @typescript-eslint/no-unused-vars
export async function getDiskType(filePath: string): Promise<DriveInfo | null> {
    return null; // Temporary disabled

    /*const normalizedPath = pathModule.resolve(filePath)
    return await getDiskTypeWindows(normalizedPath)*/
}

export async function getDiskTypeWindows(filePath: string): Promise<DriveInfo | null> {
    const driveLetter = pathModule.parse(filePath).root.split("\\").join("");
    const command = `wmic logicaldisk where "DeviceID='${driveLetter}'" get DriveType`;

    const stdout = await execCommand(command);
    const lines = stdout.trim().split("\n");

    if (lines.length < 2 || !lines[1]) {
        return null;
    }

    const driveType = lines[1].trim();

    if (!driveType) {
        return null;
    }

    const isPhysical = driveType.trim() === "3";
    const isExternal = !isPhysical;

    return {
        isPhysical,
        isExternal,
    };
}

export async function getLocalDirectorySize(path: string): Promise<{
    items: number;
    size: number;
}> {
    return new Promise<{
        items: number;
        size: number;
        // eslint-disable-next-line no-async-promise-executor
    }>(async (resolve, reject) => {
        try {
            let didError = false;
            let didErrorErr: Error = new Error("Could not read local directory.");
            let size = 0;
            let items = 0;
            const stream = FastGlob.stream("**/*", {
                dot: true,
                onlyDirectories: false,
                onlyFiles: false,
                throwErrorOnBrokenSymbolicLink: false,
                cwd: path,
                followSymbolicLinks: false,
                deep: Infinity,
                fs,
                suppressErrors: true,
                stats: true,
                unique: true,
                objectMode: true,
                ignore: [
                    "**/.filen.trash.local/**/*",
                    "**/$RECYCLE.BIN/**/*",
                    "**/System Volume Information/**/*",
                ],
            });

            stream.on("error", (err) => {
                didError = true;
                didErrorErr = err;

                reject(err);
            });

            if (didError) {
                reject(didErrorErr);

                return;
            }

            for await (const entry of stream) {
                if (didError) {
                    break;
                }

                const entryItem = entry as unknown as Required<Entry>;

                if (entryItem.stats && entryItem.dirent?.isFile()) {
                    size += entryItem.stats.size;
                }

                items += 1;
            }

            if (didError) {
                reject(didErrorErr);

                return;
            }

            resolve({
                items,
                size,
            });
        } catch (e) {
            reject(e);
        }
    });
}
