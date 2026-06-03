import { execFile } from "child_process";
import crypto from "node:crypto";
import path from "node:path";
import { promisify } from "util";
import fse from "fs-extra";

const execFileAsync = promisify(execFile);

export const MOD_DOWNLOAD_METADATA_FILE_NAME = "nhd.json";

type DownloadSource = "mod" | "gamebanana";

interface ModDownloadMetadataBase {
    id: string;
    source: DownloadSource;
    downloadedAt: string;
}

export interface GameBananaModDownloadMetadata extends ModDownloadMetadataBase {
    source: "gamebanana";
    mod: {
        id: number;
        pageUrl: string;
        version: string | null;
    };
    author: {
        name: string | null;
        url: string | null;
    };
    file: {
        downloadUrl: string;
        md5: string | null;
    };
}

export interface DirectModDownloadMetadata extends ModDownloadMetadataBase {
    source: "mod";
}

export type ModDownloadMetadata = DirectModDownloadMetadata | GameBananaModDownloadMetadata;

export type ModDownloadMetadataInput =
    | Omit<DirectModDownloadMetadata, "id">
    | Omit<GameBananaModDownloadMetadata, "id">;

export async function writeModDownloadMetadata(
    dirPath: string,
    metadata: ModDownloadMetadataInput,
) {
    await fse.ensureDir(dirPath);
    const data = { id: crypto.randomUUID(), ...metadata } as ModDownloadMetadata;
    const metadataPath = path.join(dirPath, MOD_DOWNLOAD_METADATA_FILE_NAME);
    await fse.writeJson(metadataPath, data, { spaces: 2 });
    await hideFile(metadataPath);
}

export async function writeModDownloadMetadataToDirectories(
    paths: string[],
    metadata: ModDownloadMetadataInput,
) {
    const directories = new Set<string>();

    for (const targetPath of paths) {
        const stat = await fse.stat(targetPath);
        directories.add(stat.isDirectory() ? targetPath : path.dirname(targetPath));
    }

    await Promise.all(
        Array.from(directories).map((directoryPath) =>
            writeModDownloadMetadata(directoryPath, metadata),
        ),
    );
}

async function hideFile(filePath: string) {
    await execFileAsync("attrib", ["+h", filePath], { windowsHide: true });
}
