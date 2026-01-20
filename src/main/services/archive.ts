import { NahidaDesktop } from "..";
import path from "node:path";
import fse from "fs-extra";
import unzipper from "unzipper";
import { createExtractorFromFile } from "node-unrar-js";
import Seven from "node-7z";

export class ArchiveService {
    private readonly desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    async list(archivePath: string): Promise<string[]> {
        const ext = path.extname(archivePath).toLowerCase();

        if (ext === ".zip") {
            const directory = await unzipper.Open.file(archivePath);
            return directory.files.map((f) => f.path);
        } else if (ext === ".rar") {
            const extractor = await createExtractorFromFile({
                filepath: archivePath,
            });
            const list = extractor.getFileList();
            return [...list.fileHeaders].map((h) => h.name);
        } else if (ext === ".7z") {
            return new Promise((resolve, reject) => {
                const stream = Seven.list(archivePath);
                const files: string[] = [];

                stream.on("data", (data: any) => {
                    files.push(data.file || data.path);
                });

                stream.on("end", () => {
                    resolve(files);
                });

                stream.on("error", (err: Error) => {
                    reject(err);
                });
            });
        } else {
            throw new Error(`Unsupported archive format: ${ext}`);
        }
    }

    async extract(archivePath: string, targetDir: string): Promise<void> {
        const ext = path.extname(archivePath).toLowerCase();
        await fse.ensureDir(targetDir);

        if (ext === ".zip") {
            await this.extractZip(archivePath, targetDir);
        } else if (ext === ".rar") {
            await this.extractRar(archivePath, targetDir);
        } else if (ext === ".7z") {
            await this.extract7z(archivePath, targetDir);
        } else {
            throw new Error(`Unsupported archive format: ${ext}`);
        }
    }

    private async extractZip(archivePath: string, targetPath: string): Promise<void> {
        await fse.ensureDir(targetPath);
        const directory = await unzipper.Open.file(archivePath);
        await directory.extract({ path: targetPath });
    }

    private async extractRar(archivePath: string, targetPath: string): Promise<void> {
        await fse.ensureDir(targetPath);
        const extractor = await createExtractorFromFile({
            filepath: archivePath,
            targetPath,
        });

        const extracted = extractor.extract();
        const files = [...extracted.files];

        for (const file of files) {
            if (file.fileHeader && !file.fileHeader.flags.directory) {
                const destPath = path.join(targetPath, file.fileHeader.name);
                await fse.ensureDir(path.dirname(destPath));
                if (file.extraction) {
                    await fse.writeFile(destPath, Buffer.from(file.extraction));
                }
            }
        }
    }

    private async extract7z(archivePath: string, targetPath: string): Promise<void> {
        await fse.ensureDir(targetPath);

        return new Promise((resolve, reject) => {
            const stream = Seven.extractFull(archivePath, targetPath, {
                $progress: true,
            });

            stream.on("end", () => {
                resolve();
            });

            stream.on("error", (err: Error) => {
                reject(err);
            });
        });
    }
}

export default ArchiveService;
