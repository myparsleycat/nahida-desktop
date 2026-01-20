import { NahidaDesktop } from "..";
import path from "node:path";
import fse from "fs-extra";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { app } from "electron";

const execFileAsync = promisify(execFile);

export class ArchiveService {
    private readonly desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    async extract(archivePath: string, targetDir: string) {
        await fse.ensureDir(targetDir);

        const extractorPath = this.getExtractorPath();

        if (!fse.existsSync(extractorPath)) {
            throw new Error(
                `Extractor binary not found at: ${extractorPath}. Please ensure the application is built correctly.`,
            );
        }

        try {
            const { stderr } = await execFileAsync(extractorPath, [archivePath, targetDir], {
                maxBuffer: 10 * 1024 * 1024,
            });

            if (stderr && stderr.trim()) {
                try {
                    const errorData = JSON.parse(stderr);
                    throw new Error(`Extraction failed: ${errorData.message || errorData.error}`);
                } catch {
                    throw new Error(`Extraction failed: ${stderr}`);
                }
            }
        } catch (error: any) {
            if (error.code === "ENOENT") {
                throw new Error(`Extractor binary not found: ${extractorPath}`);
            }
            throw new Error(`Failed to extract archive: ${error.message}`);
        }
    }

    private getExtractorPath(): string {
        if (app.isPackaged) {
            return path.join(app.getAppPath(), "..", "extractor.exe");
        }
        return path.join(app.getAppPath(), "build", "extractor", "extractor.exe");
    }
}

export default ArchiveService;
